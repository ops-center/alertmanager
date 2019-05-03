package alertmanager

import (
	"flag"
	"time"

	"github.com/spf13/pflag"
)


// MultitenantAlertmanagerConfig is the configuration for a multitenant Alertmanager.
type MultitenantAlertmanagerConfig struct {
	DataDir       string
	Retention     time.Duration
	ExternalURL   string
	ConfigsAPIURL string
	PollInterval  time.Duration
	ClientTimeout time.Duration

	ClusterBindAddr      string
	ClusterAdvertiseAddr string

	Peers                []string
	PeerTimeout          time.Duration
	GossipInterval       time.Duration
	PushPullInterval     time.Duration
	TcpTimeout           time.Duration
	ProbeTimeout         time.Duration
	ProbeInterval        time.Duration
	SettleTimeout        time.Duration
	ReconnectInterval    time.Duration
	PeerReconnectTimeout time.Duration
}

// AddFlags adds the flags required to config this to the given FlagSet.
func (cfg *MultitenantAlertmanagerConfig) AddFlags(f *pflag.FlagSet) {
	flag.StringVar(&cfg.DataDir, "alertmanager.storage.path", "data/", "Base path for data storage.")
	flag.DurationVar(&cfg.Retention, "alertmanager.storage.retention", 5*24*time.Hour, "How long to keep data for.")

	flag.StringVar(&cfg.ExternalURL, "alertmanager.web.external-url", "/api/prom/alertmanager", "The URL under which Alertmanager is externally reachable (for example, if Alertmanager is served via a reverse proxy). Used for generating relative and absolute links back to Alertmanager itself. If the URL has a path portion, it will be used to prefix all HTTP endpoints served by Alertmanager. If omitted, relevant URL components will be derived automatically.")

	// flag.Var(&cfg.ConfigsAPIURL, "alertmanager.configs.url", "URL of configs API server.")
	flag.DurationVar(&cfg.PollInterval, "alertmanager.configs.poll-interval", 15*time.Second, "How frequently to poll Cortex configs")
	flag.DurationVar(&cfg.ClientTimeout, "alertmanager.configs.client-timeout", 5*time.Second, "Timeout for requests to Weave Cloud configs service.")

	flag.StringVar(&cfg.ClusterBindAddr, "cluster.listen-address", "0.0.0.0:9094", "Listen address for cluster.")
	// TODO: add cluster flags
}

func (c *MultitenantAlertmanagerConfig) Validate() error {
	return nil
}