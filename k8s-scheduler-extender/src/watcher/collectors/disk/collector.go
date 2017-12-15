package disk

import (
	"os"
	"path/filepath"
	"time"

	"common"
	diskclient "resource/disk/client"
	diskcontroller "resource/disk/controller"
	disktype "resource/disk/types"
	"util"
	"watcher/configure"

	"github.com/golang/glog"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Disk Collector
type DiskCollector struct {
	diskController *diskcontroller.DiskController
	inspectPath    string
	waitGroup      util.WaitGroupWrapper
	checkInterval  int
	stopChan       chan bool
	nodeName       string
}

// New Disk Collector
func NewDiskCollector() (*DiskCollector, error) {
	// extensions client
	extensionsClient, err := util.CreateApiExtensionsClient(configure.Conf.KubeConfig)
	if err != nil {
		glog.Fatalf("Error creating extensions client: %v\n", err)
		return nil, err
	}

	diskController, err := diskcontroller.NewDiskController(extensionsClient, configure.Conf.KubeConfig)
	if err != nil {
		return nil, err
	}

	if err := diskController.Run(); err != nil {
		return nil, err
	}

	return &DiskCollector{
		diskController: diskController,
		inspectPath:    configure.Conf.DiskCollectorInspectPath,
		checkInterval:  configure.Conf.DiskCollectorCheckInterval,
		stopChan:       make(chan bool),
		nodeName:       os.Getenv(common.NodeEnvName),
	}, nil
}

// Start
func (c *DiskCollector) Run() error {
	c.waitGroup.Wrap(func() {
		c.Start()
	})

	return nil
}

// Run
func (c *DiskCollector) Start() {
	for {
		select {
		case <-time.After(time.Second * time.Duration(c.checkInterval)):
			glog.Infof("------ timer disk mount check ------")
			// find new mount
			c.discoveryMount()
			// delete not exist mount
			c.deleteMount()
		case <-c.stopChan:
			break
		}
	}
}

// discover
func (c *DiskCollector) discoveryMount() error {
	files, err := util.ReadDir(c.inspectPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		filePath := filepath.Join(c.inspectPath, file)
		diskName := c.nodeName + file
		_, isExist := c.diskController.Cache.GetDisk(diskName)
		if !isExist {
			disk := c.makeDiskResource(diskName, filePath)
			for {
				err := diskclient.InsertDiskInstance(c.diskController.DiskClient, disk)
				if err == nil || apierrors.IsAlreadyExists(err) {
					break
				}
			}
		}
	}

	return nil
}

// check if mount deleted
func (c *DiskCollector) deleteMount() error {
	files, err := util.ReadDir(c.inspectPath)
	if err != nil {
		return err
	}

	disks := c.diskController.Cache.GetDisksByNode(c.nodeName)
	for _, disk := range disks {
		if !c.checkElemExistSlice(disk.ObjectMeta.Name, files) {
			err := diskclient.DeleteDiskInstance(c.diskController.DiskClient, disk.ObjectMeta.Name)
			if err != nil {
				glog.Infof("disk collector delete disk resource error : %v", err)
				return err
			}
		}
	}

	return nil
}

// check elem exist the slice
func (c *DiskCollector) checkElemExistSlice(diskName string, files []string) bool {
	for _, v := range files {
		if diskName == (c.nodeName + v) {
			return true
		}
	}

	return false
}

// make disk resource
func (c *DiskCollector) makeDiskResource(diskName, mountPath string) *disktype.Disk {
	return &disktype.Disk{
		ObjectMeta: metav1.ObjectMeta{
			Name: diskName,
		},
		Spec: disktype.DiskSpec{
			Type:     "SSD",
			Node:     c.nodeName,
			Path:     mountPath,
			Capacity: 1000000,
		},
		Status: disktype.DiskStatus{
			State: disktype.DiskStateRunning,
		},
	}
}

// Stop
func (c *DiskCollector) Stop() {
	// stop disk controller
	c.diskController.Stop()
}
