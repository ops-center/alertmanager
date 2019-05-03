package alertmanager

import "time"

type AlertmanagerConfig struct {
	// TODO: Add id for containing multiple config for single user

	Config              string            `json:"config"`
	TemplateFiles       map[string]string `json:"templateFiles,omitempty"`
	UpdatedAtInUnix     int64             `json:"updatedAtInUnix,omitempty"`
	DeactivatedAtInUnix int64             `json:"deactivatedAtInUnix,omitempty"`
}

type AlertmanagerClient interface {
	GetConfig(userID string) (AlertmanagerConfig, error)
	GetAllConfigs() (map[string]AlertmanagerConfig, error)
	GetAllConfigsUpdatedOrDeletedAfter(unixTime int64) (map[string]AlertmanagerConfig, error)

	SetConfig(userID string, config AlertmanagerConfig) error

	DeactivateConfig(userID string) error

	RestoreConfig(userID string) error
}

type Inmem struct {
	storage map[string]AlertmanagerConfig
}

func NewInmemAlertmanagerConfigStore() AlertmanagerClient {
	return &Inmem{
		storage: map[string]AlertmanagerConfig{},
	}
}

func (m *Inmem) GetConfig(userID string) (AlertmanagerConfig, error) {
	if a, ok := m.storage[userID]; ok {
		if a.DeactivatedAtInUnix > 0 {
			return AlertmanagerConfig{}, nil
		}
		return a, nil
	}
	return AlertmanagerConfig{}, nil
}

func (m *Inmem) GetAllConfigs() (map[string]AlertmanagerConfig, error) {
	resp := map[string]AlertmanagerConfig{}
	for uid, a := range m.storage {
		if a.DeactivatedAtInUnix <= 0 {
			resp[uid] = a
		}
	}
	return resp, nil
}

func (m *Inmem) GetAllConfigsUpdatedOrDeletedAfter(after int64) (map[string]AlertmanagerConfig, error) {
	resp := map[string]AlertmanagerConfig{}
	for uid, a := range m.storage {
		if a.DeactivatedAtInUnix <= 0 && a.UpdatedAtInUnix > after {
			resp[uid] = a
		}
	}
	return resp, nil
}

func (m *Inmem) SetConfig(userID string, config AlertmanagerConfig) error {
	config.UpdatedAtInUnix = time.Now().Unix()
	m.storage[userID] = config
	return nil
}

func (m *Inmem) DeactivateConfig(userID string) error {
	if a, ok := m.storage[userID]; ok {
		a.DeactivatedAtInUnix = time.Now().Unix()
	}
	return nil
}

func (m *Inmem) RestoreConfig(userID string) error {
	if a, ok := m.storage[userID]; ok {
		a.UpdatedAtInUnix = time.Now().Unix()
		a.DeactivatedAtInUnix = 0
	}
	return nil
}
