package alertmanager

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/searchlight/alertmanager/pkg/logger"

	"github.com/go-kit/kit/log/level"
	amconfig "github.com/prometheus/alertmanager/config"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/weaveworks/common/instrument"
)

var backoffConfig = util.BackoffConfig{
	// Backoff for loading initial configuration set.
	MinBackoff: 100 * time.Millisecond,
	MaxBackoff: 2 * time.Second,
}

var (
	totalConfigs = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "configs",
		Help:      "How many configs the multitenant alertmanager knows about.",
	})
	configsRequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "configs_request_duration_seconds",
		Help:      "Time spent requesting configs.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation", "status_code"}))
	totalPeers = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "mesh_peers",
		Help:      "Number of peers the multitenant alertmanager knows about",
	})
)

func init() {
	configsRequestDuration.Register()
	prometheus.MustRegister(totalConfigs)
	prometheus.MustRegister(totalPeers)
}

// A MultitenantAlertmanager manages Alertmanager instances for multiple
// organizations.
type MultitenantAlertmanager struct {
	cfg *MultitenantAlertmanagerConfig

	configsClient AlertmanagerClient

	// All the organization configurations that we have. Only used for instrumentation.
	cfgs                 map[string]AlertmanagerConfig
	configsUpdatedAtUnix int64
	cfgMutex             sync.RWMutex

	alertmanagersMtx sync.Mutex
	alertmanagers    map[string]*Alertmanager

	stop chan struct{}
	done chan struct{}
}

// NewMultitenantAlertmanager creates a new MultitenantAlertmanager.
func NewMultitenantAlertmanager(cfg *MultitenantAlertmanagerConfig, configClient AlertmanagerClient) (*MultitenantAlertmanager, error) {
	err := os.MkdirAll(cfg.DataDir, 0777)
	if err != nil {
		return nil, fmt.Errorf("unable to create Alertmanager data directory %q: %s", cfg.DataDir, err)
	}

	am := &MultitenantAlertmanager{
		cfg:           cfg,
		configsClient: configClient,
		cfgs:          map[string]AlertmanagerConfig{},
		alertmanagers: map[string]*Alertmanager{},
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	return am, nil
}

// Run the MultitenantAlertmanager.
func (am *MultitenantAlertmanager) Run() {
	defer close(am.done)

	// Load initial set of all configurations before polling for new ones.
	am.addNewConfigs(am.loadAllConfigs())
	ticker := time.NewTicker(am.cfg.PollInterval)
	for {
		select {
		case <-ticker.C:
			err := am.updateConfigs()
			if err != nil {
				level.Warn(logger.Logger).Log("msg", "MultitenantAlertmanager: error updating configs", "err", err)
			}
		case <-am.stop:
			ticker.Stop()
			return
		}
	}
}

// Stop stops the MultitenantAlertmanager.
func (am *MultitenantAlertmanager) Stop() {
	close(am.stop)
	<-am.done
	for _, am := range am.alertmanagers {
		am.Stop()
	}
	level.Debug(logger.Logger).Log("msg", "MultitenantAlertmanager stopped")
}

// Load the full set of configurations from the server, retrying with backoff
// until we can get them.
func (am *MultitenantAlertmanager) loadAllConfigs() map[string]AlertmanagerConfig {
	backoff := util.NewBackoff(context.Background(), backoffConfig)
	for {
		cfgs, err := am.poll()
		if err == nil {
			level.Debug(logger.Logger).Log("msg", "MultitenantAlertmanager: initial configuration load", "num_configs", len(cfgs))
			return cfgs
		}
		level.Warn(logger.Logger).Log("msg", "MultitenantAlertmanager: error fetching all configurations, backing off", "err", err)
		backoff.Wait()
	}
}

func (am *MultitenantAlertmanager) updateConfigs() error {
	cfgs, err := am.poll()
	if err != nil {
		return err
	}
	am.addNewConfigs(cfgs)
	return nil
}

// poll the configuration server. Not re-entrant.
func (am *MultitenantAlertmanager) poll() (map[string]AlertmanagerConfig, error) {
	var cfgs map[string]AlertmanagerConfig
	err := instrument.CollectedRequest(context.Background(), "Configs.GetAlertmanagerConfigs", configsRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		var err error
		cfgs, err = am.configsClient.GetAllConfigsUpdatedOrDeletedAfter(am.configsUpdatedAtUnix)
		return err
	})
	if err != nil {
		level.Warn(logger.Logger).Log("msg", "MultitenantAlertmanager: configs server poll failed", "err", err)
		return nil, err
	}
	am.cfgMutex.Lock()
	am.configsUpdatedAtUnix = getLatestUpdateTime(cfgs, am.configsUpdatedAtUnix)
	am.cfgMutex.Unlock()
	return cfgs, nil
}

func (am *MultitenantAlertmanager) addNewConfigs(cfgs map[string]AlertmanagerConfig) {
	// TODO: instrument how many configs we have, both valid & invalid.
	level.Debug(logger.Logger).Log("msg", "adding configurations", "num_configs", len(cfgs))
	for userID, config := range cfgs {

		err := am.setConfig(userID, &config)
		if err != nil {
			level.Warn(logger.Logger).Log("msg", "MultitenantAlertmanager: error applying config", "err", err)
			continue
		}

	}
	totalConfigs.Set(float64(len(am.cfgs)))
}

func (am *MultitenantAlertmanager) createTemplatesFile(userID, fn, content string) (bool, error) {
	dir := filepath.Join(am.cfg.DataDir, "templates", userID, filepath.Dir(fn))
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return false, fmt.Errorf("unable to create Alertmanager templates directory %q: %s", dir, err)
	}

	file := filepath.Join(dir, fn)
	// Check if the template file already exists and if it has changed
	if tmpl, err := ioutil.ReadFile(file); err == nil && string(tmpl) == content {
		return false, nil
	}

	if err := ioutil.WriteFile(file, []byte(content), 0644); err != nil {
		return false, fmt.Errorf("unable to create Alertmanager template file %q: %s", file, err)
	}

	return true, nil
}

// setConfig applies the given configuration to the alertmanager for `userID`,
// creating an alertmanager if it doesn't already exist.
func (am *MultitenantAlertmanager) setConfig(userID string, config *AlertmanagerConfig) error {
	if config == nil {
		return fmt.Errorf("alertmanager config is nil for user %v", userID)
	}

	// if deleted, then stop the alertmanager and delete config
	if config.DeactivatedAtInUnix > 0 {
		if a, ok := am.alertmanagers[userID]; ok {
			a.Stop()
			am.alertmanagersMtx.Lock()
			delete(am.alertmanagers, userID)
			am.alertmanagersMtx.Unlock()
		}

		if _, ok := am.cfgs[userID]; ok {
			am.cfgMutex.Lock()
			delete(am.cfgs, userID)
			am.cfgMutex.Unlock()
		}
	}
	_, hasExisting := am.alertmanagers[userID]
	var amConfig *amconfig.Config
	var err error
	var hasTemplateChanges bool

	for fn, content := range config.TemplateFiles {
		hasChanged, err := am.createTemplatesFile(userID, fn, content)
		if err != nil {
			return err
		}

		if hasChanged {
			hasTemplateChanges = true
		}
	}

	amConfig, err = amconfig.Load(config.Config)
	if err != nil {
		return fmt.Errorf("failed load alertmanager config for user %v: %v", userID, err)
	}

	// If no Alertmanager instance exists for this user yet, start one.
	if !hasExisting {
		newAM, err := am.newAlertmanager(userID, amConfig)
		if err != nil {
			return err
		}
		am.alertmanagersMtx.Lock()
		am.alertmanagers[userID] = newAM
		am.alertmanagersMtx.Unlock()
	} else if am.cfgs[userID].Config != config.Config || hasTemplateChanges {
		// If the config changed, apply the new one.
		err := am.alertmanagers[userID].ApplyConfig(userID, amConfig)
		if err != nil {
			return fmt.Errorf("unable to apply Alertmanager config for user %v: %v", userID, err)
		}
	}
	return nil
}

func (am *MultitenantAlertmanager) newAlertmanager(userID string, amConfig *amconfig.Config) (*Alertmanager, error) {
	u, err := url.Parse(am.cfg.ExternalURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse external url: %v", err)
	}
	newAM, err := NewAlertmanager(&Config{
		UserID:      userID,
		DataDir:     am.cfg.DataDir,
		Logger:      logger.Logger,
		Retention:   am.cfg.Retention,
		ExternalURL: u,
		Peer:        nil,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to start Alertmanager for user %v: %v", userID, err)
	}

	if err := newAM.ApplyConfig(userID, amConfig); err != nil {
		return nil, fmt.Errorf("unable to apply initial config for user %v: %v", userID, err)
	}
	return newAM, nil
}

// ServeHTTP serves the Alertmanager's web UI and API.
func (am *MultitenantAlertmanager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	userID, err := ExtractUserIDFromHTTPRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	am.alertmanagersMtx.Lock()
	userAM, ok := am.alertmanagers[userID]
	am.alertmanagersMtx.Unlock()
	if !ok {
		http.Error(w, fmt.Sprintf("no Alertmanager for this user ID"), http.StatusNotFound)
		return
	}
	userAM.router.ServeHTTP(w, req)
}

func getLatestUpdateTime(cfgs map[string]AlertmanagerConfig, cur int64) int64 {
	for _, c := range cfgs {
		if c.UpdatedAtInUnix > cur {
			cur = c.UpdatedAtInUnix
		}
	}
	return cur
}
