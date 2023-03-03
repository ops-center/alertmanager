package etcd

import (


	"github.com/pkg/errors"
	"github.com/spf13/pflag"
)

type Config struct {
	Endpoints []string
}

func NewConfig() *Config {
	return &Config{}
}

// AddFlags adds the flags required to config this to the given FlagSet
func (c *Config) AddFlags(f *pflag.FlagSet) {
	f.StringArrayVar(&c.Endpoints, "etcd.endpoints", []string{}, "Endpoints of Etcd cluster.")
}

func (c *Config) Validate() error {
	if len(c.Endpoints) == 0 {
		return errors.New("--etcd.endpoints must be non empty")
	}
	return nil
}
