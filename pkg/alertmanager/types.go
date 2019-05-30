package alertmanager

type AlertmanagerConfig struct {
	// TODO: Add id for containing multiple config for single user

	UserID              string            `json:"userID" yaml:"userID"`
	Config              string            `json:"config" yaml:"config"`
	TemplateFiles       map[string]string `json:"templateFiles,omitempty" yaml:"templateFiles,omitempty"`
	UpdatedAtInUnix     int64             `json:"updatedAtInUnix,omitempty" yaml:"updatedAtInUnix,omitempty"`
	DeactivatedAtInUnix int64             `json:"deactivatedAtInUnix,omitempty" yaml:"deactivatedAtInUnix,omitempty"`
	DeletedAtInUnix     int64             `json:"deletedAtInUnix,omitempty" yaml:"deletedAtInUnix,omitempty"`
}

type AlertmanagerGetter interface {
	GetAllConfigs() ([]AlertmanagerConfig, error)
	GetAllUpdatedConfigs() ([]AlertmanagerConfig, error)
}

type AlertmanagerWatcher interface {
	Watch(ch chan AlertmanagerConfig)
}

type AlertmanagerClient interface {
	GetConfig(userID string) (AlertmanagerConfig, error)
	GetAllConfigs() ([]AlertmanagerConfig, error)

	SetConfig(amCfg *AlertmanagerConfig) error

	DeactivateConfig(userID string) error

	RestoreConfig(userID string) error
}
