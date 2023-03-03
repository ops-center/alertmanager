package alertmanager

import "sync"

const (
	UpdateChannelBufferSize = 10000
)

type AlertmanagerGetterWrapper struct {
	amClient  AlertmanagerClient
	amWatcher AlertmanagerWatcher

	mtx sync.Mutex

	// TODO: keep prometheus metric for the length?
	newUpdates []AlertmanagerConfig
}

func NewAlertmanagerGetterWrapper(c AlertmanagerClient, w AlertmanagerWatcher) (AlertmanagerGetter, error) {
	amGetter := &AlertmanagerGetterWrapper{
		amClient:   c,
		amWatcher:  w,
		newUpdates: []AlertmanagerConfig{},
	}
	go amGetter.RunUpdatesCollector()

	return amGetter, nil
}

func (am *AlertmanagerGetterWrapper) GetAllConfigs() ([]AlertmanagerConfig, error) {
	return am.amClient.GetAllConfigs()
}

func (am *AlertmanagerGetterWrapper) GetAllUpdatedConfigs() ([]AlertmanagerConfig, error) {
	// slice copy
	var list []AlertmanagerConfig
	am.mtx.Lock()
	list = am.newUpdates
	am.newUpdates = []AlertmanagerConfig{}
	am.mtx.Unlock()
	return list, nil
}

func (am *AlertmanagerGetterWrapper) RunUpdatesCollector() {
	ch := make(chan AlertmanagerConfig, UpdateChannelBufferSize)
	go am.amWatcher.Watch(ch)

	for rg := range ch {
		am.mtx.Lock()
		am.newUpdates = append(am.newUpdates, rg)
		am.mtx.Unlock()
	}
}


