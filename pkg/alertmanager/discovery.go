package alertmanager

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/pkg/errors"
)

const (
	PodIPEnv        = "POD_IP"
	PodNamespaceEnv = "POD_NAMESPACE"
)

func getAdvertiseAddr(cfg *MultitenantAlertmanagerConfig) (string, error) {
	if cfg.ClusterAdvertiseAddr != "" {
		return cfg.ClusterAdvertiseAddr, nil
	}

	podIP := os.Getenv(PodIPEnv)
	if podIP == "" {
		return "", errors.New("advertise address or POD_IP env is not set")
	}

	bindPort, err := getPort(cfg.ClusterBindAddr)
	if err != nil {
		return "", errors.Wrap(err, "invalid listen address")
	}
	return fmt.Sprintf("%s:%d", podIP, bindPort), nil
}

func getPort(addr string) (int, error) {
	_, bindPortStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	bindPort, err := strconv.Atoi(bindPortStr)
	if err != nil {
		return 0, errors.Wrap(err, "invalid address port")
	}
	return bindPort, nil
}
