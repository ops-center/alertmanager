package alertmanager

import (
	"context"
	"fmt"
	"github.com/prometheus/alertmanager/cluster"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	api "github.com/prometheus/alertmanager/api/v1"
	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/dispatch"
	"github.com/prometheus/alertmanager/inhibit"
	"github.com/prometheus/alertmanager/nflog"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/provider/mem"
	"github.com/prometheus/alertmanager/silence"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
	"github.com/prometheus/alertmanager/ui"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/route"
)

const notificationLogMaintenancePeriod = 15 * time.Minute

// Config configures an Alertmanager.
type Config struct {
	UserID string
	// Used to persist notification logs and silences on disk.
	DataDir     string
	Logger      log.Logger
	Retention   time.Duration
	ExternalURL *url.URL
	Peer *cluster.Peer
}

// An Alertmanager manages the alerts for one user.
type Alertmanager struct {
	cfg        *Config
	api        *api.API
	logger     log.Logger
	nflog      *nflog.Log
	silences   *silence.Silences
	marker     types.Marker
	alerts     *mem.Alerts
	dispatcher *dispatch.Dispatcher
	inhibitor  *inhibit.Inhibitor
	stop       chan struct{}
	wg         sync.WaitGroup
	router     *route.Router
}

// New creates a new Alertmanager.
func NewAlertmanager(cfg *Config) (*Alertmanager, error) {
	am := &Alertmanager{
		cfg:    cfg,
		logger: log.With(cfg.Logger, "user", cfg.UserID),
		stop:   make(chan struct{}),
	}

	am.wg.Add(1)
	nflogID := fmt.Sprintf("nflog:%s", cfg.UserID)
	nflogOpts := []nflog.Option{
		nflog.WithRetention(cfg.Retention),
		nflog.WithSnapshot(filepath.Join(cfg.DataDir, nflogID)),
		nflog.WithMaintenance(notificationLogMaintenancePeriod, am.stop, am.wg.Done),
		// TODO: Build a registry that can merge metrics from multiple users.
		// For now, these metrics are ignored, as we can't register the same
		// metric twice with a single registry.
		nflog.WithMetrics(prometheus.NewRegistry()),
		nflog.WithLogger(log.With(am.logger, "component", "nflog")),
	}
	var err error
	am.nflog, err = nflog.New(nflogOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create notification log: %v", err)
	}
	if am.cfg.Peer != nil {
		c := am.cfg.Peer.AddState(fmt.Sprintf("nfl_%s", am.cfg.UserID), am.nflog, prometheus.DefaultRegisterer)
		am.nflog.SetBroadcast(c.Broadcast)
	}

	am.marker = types.NewMarker()

	silencesID := fmt.Sprintf("silences:%s", cfg.UserID)
	silencesOpts := silence.Options{
		SnapshotFile: filepath.Join(cfg.DataDir, silencesID),
		Retention:    cfg.Retention,
		Logger:       log.With(am.logger, "component", "silences"),
		// TODO: Build a registry that can merge metrics from multiple users.
		// For now, these metrics are ignored, as we can't register the same
		// metric twice with a single registry.
		Metrics: prometheus.NewRegistry(),
	}

	am.silences, err = silence.New(silencesOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create silences: %v", err)
	}
	if am.cfg.Peer != nil {
		c := am.cfg.Peer.AddState(fmt.Sprintf("sil_%s", am.cfg.UserID), am.nflog, prometheus.DefaultRegisterer)
		am.silences.SetBroadcast(c.Broadcast)
	}

	am.wg.Add(1)
	go func() {
		am.silences.Maintenance(15*time.Minute, filepath.Join(cfg.DataDir, silencesID), am.stop)
		am.wg.Done()
	}()

	marker := types.NewMarker()
	am.alerts, err = mem.NewAlerts(context.Background(), marker, 30*time.Minute, am.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create alerts: %v", err)
	}

	am.api = api.New(
		am.alerts,
		am.silences,
		marker.Status,
		// TODO: look at this
		nil, // Passing a nil mesh router since we don't show mesh router information in Cortex anyway.
		log.With(am.logger, "component", "api"),
	)

	am.router = route.New()

	webReload := make(chan chan error)
	ui.Register(am.router.WithPrefix(am.cfg.ExternalURL.Path), webReload, log.With(am.logger, "component", "ui"))
	am.api.Register(am.router.WithPrefix(path.Join(am.cfg.ExternalURL.Path, "/api/v1")))

	go func() {
		for {
			select {
			// Since this is not a "normal" Alertmanager which reads its config
			// from disk, we just ignore web-based reload signals. Config updates are
			// only applied externally via ApplyConfig().
			case <-webReload:
			case <-am.stop:
				return
			}
		}
	}()

	return am, nil
}

// ApplyConfig applies a new configuration to an Alertmanager.
func (am *Alertmanager) ApplyConfig(userID string, conf *config.Config) error {
	var (
		tmpl     *template.Template
		pipeline notify.Stage
	)

	templateFiles := make([]string, len(conf.Templates), len(conf.Templates))
	if len(conf.Templates) > 0 {
		for i, t := range conf.Templates {
			templateFiles[i] = filepath.Join(am.cfg.DataDir, "templates", userID, t)
		}
	}

	tmpl, err := template.FromGlobs(templateFiles...)
	if err != nil {
		return err
	}
	tmpl.ExternalURL = am.cfg.ExternalURL

	err = am.api.Update(conf, time.Duration(conf.Global.ResolveTimeout))
	if err != nil {
		return err
	}

	am.inhibitor.Stop()
	am.dispatcher.Stop()

	am.inhibitor = inhibit.NewInhibitor(am.alerts, conf.InhibitRules, am.marker, log.With(am.logger, "component", "inhibitor"))

	waitFunc := func() time.Duration { return 0 }
	if am.cfg.Peer != nil {
		// TODO: use flag peerTimeout
		waitFunc = clusterWait(am.cfg.Peer, 15*time.Second)
	}
	timeoutFunc := func(d time.Duration) time.Duration {
		if d < notify.MinTimeout {
			d = notify.MinTimeout
		}
		return d + waitFunc()
	}

	pipeline = notify.BuildPipeline(
		conf.Receivers,
		tmpl,
		waitFunc,
		am.inhibitor,
		am.silences,
		am.nflog,
		am.marker,
		am.cfg.Peer,
		log.With(am.logger, "component", "pipeline"),
	)
	am.dispatcher = dispatch.NewDispatcher(
		am.alerts,
		dispatch.NewRoute(conf.Route, nil),
		pipeline,
		am.marker,
		timeoutFunc,
		log.With(am.logger, "component", "dispatcher"),
	)

	go am.dispatcher.Run()
	go am.inhibitor.Run()

	return nil
}

// Stop stops the Alertmanager.
func (am *Alertmanager) Stop() {
	am.dispatcher.Stop()
	am.alerts.Close()
	close(am.stop)
	am.wg.Wait()
}

// ServeHTTP serves the Alertmanager's web UI and API.
func (am *Alertmanager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	am.router.ServeHTTP(w, req)
}

// https://github.com/prometheus/alertmanager/blob/e6d0803746482f58b44fa55d17908e6d43bee7ee/cmd/alertmanager/main.go#L477
// clusterWait returns a function that inspects the current peer state and returns
// a duration of one base timeout for each peer with a higher ID than ourselves.
func clusterWait(p *cluster.Peer, timeout time.Duration) func() time.Duration {
	return func() time.Duration {
		return time.Duration(p.Position()) * timeout
	}
}