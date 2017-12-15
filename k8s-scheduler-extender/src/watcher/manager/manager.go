package manager

import (
	"github.com/golang/glog"
	"watcher/collectors/disk"
)

type WatcherManager struct {
	DiskCollector *disk.DiskCollector
}

// init manager
func NewWatcherManager() (*WatcherManager, error) {
	diskCollector, err := disk.NewDiskCollector()
	if err != nil {
		return nil, err
	}

	return &WatcherManager{
		DiskCollector: diskCollector,
	}, nil
}

// run collector manager
func (m *WatcherManager) Run() error {
	if err := m.DiskCollector.Run(); err != nil {
		glog.Error(err)
		return err
	}

	return nil
}

// stop manager
func (m *WatcherManager) Stop() {
	// stop disk collector
	m.DiskCollector.Stop()
}
