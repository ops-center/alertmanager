package alertmanager

import (
	"fmt"
	"time"

	"github.com/prometheus/alertmanager/cluster"

	"github.com/spf13/pflag"
)

// MultitenantAlertmanagerConfig is the configuration for a multitenant Alertmanager.
type MultitenantAlertmanagerConfig struct {
	APIPort       string
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
	f.StringVar(&cfg.APIPort, "alertmanager.api-port", "9094", "API port for alertmanager.")
	f.StringVar(&cfg.DataDir, "alertmanager.storage.path", "data/", "Base path for data storage.")
	f.DurationVar(&cfg.Retention, "alertmanager.storage.retention", 5*24*time.Hour, "How long to keep data for.")

	f.StringVar(&cfg.PathPrefix, "alertmanager.path-prefix", "/api/prom/alertmanager", "This path will be used to prefix all HTTP endpoints served by Alertmanager.")

	// f.Var(&cfg.ConfigsAPIURL, "alertmanager.configs.url", "URL of configs API server.")
	f.DurationVar(&cfg.PollInterval, "alertmanager.configs.poll-interval", 15*time.Second, "How frequently to poll users alertmanager configs")
	f.DurationVar(&cfg.ClientTimeout, "alertmanager.configs.client-timeout", 5*time.Second, "Timeout for requests to users alertmanager configs service.")

	f.StringVar(&cfg.ClusterBindAddr, "cluster.listen-address", "0.0.0.0:9094", "Listen address for cluster.")
	f.StringVar(&cfg.ClusterAdvertiseAddr, "cluster.advertise-address", "", "Explicit address to advertise in cluster.")
	f.StringArrayVar(&cfg.Peers, "cluster.peer", []string{}, "Initial peers (may be repeated).")
	f.DurationVar(&cfg.PeerTimeout, "cluster.peer-timeout", 15*time.Second, "Time to wait between peers to send notifications.")
	f.DurationVar(&cfg.GossipInterval, "cluster.gossip-interval", cluster.DefaultGossipInterval, "Interval between sending gossip messages. By lowering this value (more frequent) gossip messages are propagated across the cluster more quickly at the expense of increased bandwidth.")
	f.DurationVar(&cfg.PushPullInterval, "cluster.pushpull-interval", cluster.DefaultPushPullInterval, "Interval for gossip state syncs. Setting this interval lower (more frequent) will increase convergence speeds across larger clusters at the expense of increased bandwidth usage.")
	f.DurationVar(&cfg.TcpTimeout, "cluster.tcp-timeout", cluster.DefaultTcpTimeout, "Timeout for establishing a stream connection with a remote node for a full state sync, and for stream read and write operations.")
	f.DurationVar(&cfg.ProbeTimeout, "cluster.probe-timeout", cluster.DefaultProbeTimeout, "Timeout to wait for an ack from a probed node before assuming it is unhealthy. This should be set to 99-percentile of RTT (round-trip time) on your network.")
	f.DurationVar(&cfg.ProbeInterval, "cluster.probe-interval", cluster.DefaultProbeInterval, "Interval between random node probes. Setting this lower (more frequent) will cause the cluster to detect failed nodes more quickly at the expense of increased bandwidth usage.")
	f.DurationVar(&cfg.SettleTimeout, "cluster.settle-timeout", cluster.DefaultPushPullInterval, "Maximum time to wait for cluster connections to settle before evaluating notifications.")
	f.DurationVar(&cfg.ReconnectInterval, "cluster.reconnect-interval", cluster.DefaultReconnectInterval, "Interval between attempting to reconnect to lost peers.")
	f.DurationVar(&cfg.PeerReconnectTimeout, "cluster.reconnect-timeout", cluster.DefaultReconnectTimeout, "Length of time to attempt to reconnect to a lost peer.")
}

func (c *MultitenantAlertmanagerConfig) Validate() error {
	if len(c.Peers) > 0 {
		if c.ClusterAdvertiseAddr == "" {
			return fmt.Errorf("in cluster setup, cluster.advertise-address must be provided")
		}
	}
	return nil
}
