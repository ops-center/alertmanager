package etcd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	am "github.com/searchlight/alertmanager/pkg/alertmanager"
	"go.etcd.io/etcd/clientv3"
	"gopkg.in/yaml.v2"
)

const (
	alertmanagerCfgPrefix = "alertmanager/configs/"
	keyFmt                = "alertmanager/configs/user/%s"

	DialTimeout = 10 * time.Second
)

type Client struct {
	cl     *clientv3.Client
	kv     clientv3.KV
	ctx    context.Context
	logger log.Logger
}

func NewClient(c *Config, l log.Logger) (*Client, error) {
	cl, err := clientv3.New(clientv3.Config{
		Endpoints:   c.Endpoints,
		DialTimeout: DialTimeout,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create etcd client")
	}

	// TODO: should we use context with timer, if we use, then consider the time to get all configs
	return &Client{
		cl:     cl,
		kv:     clientv3.NewKV(cl),
		ctx:    context.Background(),
		logger: l,
	}, nil
}

func (c *Client) GetConfig(userID string) (am.AlertmanagerConfig, error) {
	return c.get(getKey(userID))
}

func (c *Client) GetAllConfigs() ([]am.AlertmanagerConfig, error) {
	return c.getWithPrefix(alertmanagerCfgPrefix)
}

func (c *Client) SetConfig(amCfg *am.AlertmanagerConfig) error {
	// TODO: Add validation
	return c.put(amCfg)
}

func (c *Client) DeactivateConfig(userID string) error {
	amCfg, err := c.GetConfig(userID)
	if err != nil {
		return errors.Wrap(err, "failed to get config")
	}

	amCfg.DeactivatedAtInUnix = time.Now().Unix()
	amCfg.UpdatedAtInUnix = time.Now().Unix()

	err = c.put(&amCfg)
	if err != nil {
		return errors.Wrap(err, "failed to store config")
	}
	return nil
}

func (c *Client) RestoreConfig(userID string) error {
	amCfg, err := c.GetConfig(userID)
	if err != nil {
		return errors.Wrap(err, "failed to get config")
	}

	amCfg.DeactivatedAtInUnix = 0
	amCfg.UpdatedAtInUnix = time.Now().Unix()

	err = c.put(&amCfg)
	if err != nil {
		return errors.Wrap(err, "failed to store config")
	}
	return nil
}

func (c *Client) get(key string) (am.AlertmanagerConfig, error) {
	rg := am.AlertmanagerConfig{}

	resp, err := c.kv.Get(c.ctx, key)
	if err != nil {
		return rg, err
	}
	if len(resp.Kvs) == 0 {
		return rg, nil
	}

	if err := yaml.Unmarshal(resp.Kvs[0].Value, &rg); err != nil {
		return rg, errors.Wrap(err, "failed to decode response")
	}
	return rg, nil
}

func (c *Client) getWithPrefix(prefix string) ([]am.AlertmanagerConfig, error) {
	resp, err := c.kv.Get(c.ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}

	amCfgList := []am.AlertmanagerConfig{}
	for _, rg := range resp.Kvs {
		amCfg := am.AlertmanagerConfig{}
		if err := yaml.Unmarshal(rg.Value, &amCfg); err != nil {
			return nil, errors.Wrap(err, "failed to decode response")
		}
		amCfgList = append(amCfgList, amCfg)
	}
	return amCfgList, nil
}

func (c *Client) put(amCfg *am.AlertmanagerConfig) error {
	data, err := yaml.Marshal(amCfg)
	if err != nil {
		return errors.Wrap(err, "failed to marshal alertmanager config")
	}

	_, err = c.kv.Put(c.ctx, getKey(amCfg.UserID), string(data))
	if err != nil {
		return errors.Wrap(err, "failed to store alertmanager config")
	}
	return nil
}

func (c *Client) delete(key string) error {
	// TODO: should delete it or just set the delete timestamp.
	_, err := c.kv.Delete(c.ctx, key)
	if err != nil {
		return errors.Wrap(err, "failed to delete rule group")
	}
	return nil
}

// Watches the keys
// it's blocking
func (c *Client) Watch(ch chan am.AlertmanagerConfig) {
	watcher := c.cl.Watch(c.ctx, alertmanagerCfgPrefix, clientv3.WithPrefix())
	for resp := range watcher {
		for _, ev := range resp.Events {

			if ev.Type == clientv3.EventTypeDelete {
				userID := getUserIDFromKey(string(ev.Kv.Key))
				ch <- am.AlertmanagerConfig{
					UserID:          userID,
					DeletedAtInUnix: time.Now().Unix(),
				}
			} else {
				amCfg := am.AlertmanagerConfig{}
				if err := yaml.Unmarshal(ev.Kv.Value, &amCfg); err != nil {
					level.Warn(c.logger).Log("msg", "failed unmarshal response", "err", err)
				} else {
					ch <- amCfg
				}
			}
		}
	}
}

func (c *Client) Close() {
	c.cl.Close()
}

func getKey(usedID string) string {
	return fmt.Sprintf(keyFmt, usedID)
}

func getUserIDFromKey(key string) (userID string) {
	st := strings.Split(key, "/")
	if len(st) >= 4 {
		userID = st[3]
	}
	return userID
}
