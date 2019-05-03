package alertmanager

import (
	"time"

	"github.com/spf13/pflag"
)

// MultitenantAlertmanagerConfig is the configuration for a multitenant Alertmanager.
type MultitenantAlertmanagerConfig struct {
	DataDir       string
	Retention     time.Duration
	PathPrefix    string
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
	f.StringVar(&cfg.DataDir, "alertmanager.storage.path", "data/", "Base path for data storage.")
	f.DurationVar(&cfg.Retention, "alertmanager.storage.retention", 5*24*time.Hour, "How long to keep data for.")

	f.StringVar(&cfg.PathPrefix, "alertmanager.path-prefix", "/api/prom/alertmanager", "This path will be used to prefix all HTTP endpoints served by Alertmanager.")

	// f.Var(&cfg.ConfigsAPIURL, "alertmanager.configs.url", "URL of configs API server.")
	f.DurationVar(&cfg.PollInterval, "alertmanager.configs.poll-interval", 15*time.Second, "How frequently to poll users alertmanager configs")
	f.DurationVar(&cfg.ClientTimeout, "alertmanager.configs.client-timeout", 5*time.Second, "Timeout for requests to users alertmanager configs service.")

	f.StringVar(&cfg.ClusterBindAddr, "cluster.listen-address", "0.0.0.0:9094", "Listen address for cluster.")
	// TODO: add cluster flags
}

func (c *MultitenantAlertmanagerConfig) Validate() error {
	return nil
}
