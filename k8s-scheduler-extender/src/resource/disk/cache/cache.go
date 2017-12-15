package cache

import (
	"sync"

	disktype "resource/disk/types"

	"github.com/golang/glog"
)

// Cache Struct
type DiskCache struct {
	mutex sync.Mutex
	disks map[string]*disktype.Disk
}

// Init Disk Cache
func NewDiskCache() *DiskCache {
	return &DiskCache{disks: map[string]*disktype.Disk{}}
}

// Get Disk by disk name
func (cache *DiskCache) GetDisk(diskName string) (*disktype.Disk, bool) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	disk, exists := cache.disks[diskName]
	return disk, exists
}

// Get Disks by node
func (cache *DiskCache) GetDisksByNode(nodeName string) []*disktype.Disk {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	disks := []*disktype.Disk{}
	for _, disk := range cache.disks {
		if disk.Spec.Node == nodeName {
			disks = append(disks, disk)
		}
	}

	return disks
}

// Add Disk
func (cache *DiskCache) AddDisk(disk *disktype.Disk) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	cache.disks[disk.ObjectMeta.Name] = disk
	glog.Infof("Added disk %q to cache", disk.ObjectMeta.Name)
}

// Update Disk
func (cache *DiskCache) UpdateDisk(disk *disktype.Disk) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	cache.disks[disk.ObjectMeta.Name] = disk
	glog.Infof("Updated disk %q to cache", disk.ObjectMeta.Name)
}

// Delete Disk
func (cache *DiskCache) DeleteDisk(disk *disktype.Disk) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	delete(cache.disks, disk.ObjectMeta.Name)
	glog.Infof("Deleted disk %q from cache", disk.ObjectMeta.Name)
}

// List Disks
func (cache *DiskCache) ListDisks() []*disktype.Disk {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	disks := []*disktype.Disk{}
	for _, disk := range cache.disks {
		disks = append(disks, disk)
	}
	return disks
}
