package controller

import (
	diskcache "resource/disk/cache"
	diskclient "resource/disk/client"
	disktype "resource/disk/types"
	"util"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	apiv1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/golang/glog"
)

// Watcher is an disk of watching on resource create/update/delete events
type DiskController struct {
	DiskClient *rest.RESTClient
	DiskScheme *runtime.Scheme
	Crd        *apiextensionsv1beta1.CustomResourceDefinition
	Cache      *diskcache.DiskCache
	waitGroup  util.WaitGroupWrapper
	stopChan   chan struct{}
}

// New Disk Controller
func NewDiskController(client apiextensionsclient.Interface, kubeConfig string) (*DiskController, error) {
	// make a new config for our extension's API group, using the first config as a baseline
	diskClient, diskScheme, crd, err := diskclient.NewClient(client, kubeConfig)
	if err != nil {
		return nil, err
	}

	// Disk Cache
	cache := diskcache.NewDiskCache()

	return &DiskController{
		DiskClient: diskClient,
		DiskScheme: diskScheme,
		Crd:        crd,
		Cache:      cache,
		stopChan:   make(chan struct{}),
	}, nil
}

// Run starts an Disk resource controller
func (c *DiskController) Run() error {
	// Watch Disk objects
	_, err := c.watchDisks()
	if err != nil {
		glog.Errorf("Failed to register watch for Disk resource: %v\n", err)
		return err
	}
	glog.Infof("Starting Custom Resource Disk Controller")

	return nil
}

func (c *DiskController) watchDisks() (cache.Controller, error) {
	source := cache.NewListWatchFromClient(
		c.DiskClient,
		disktype.DiskResourcePlural,
		apiv1.NamespaceAll,
		fields.Everything())

	_, controller := cache.NewInformer(
		source,

		// The object type.
		&disktype.Disk{},

		// resyncPeriod
		// Every resyncPeriod, all resources in the cache will retrigger events.
		// Set to 0 to disable the resync.
		0,

		// Your custom resource event handlers.
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.onAdd,
			UpdateFunc: c.onUpdate,
			DeleteFunc: c.onDelete,
		})

	// contonler run
	c.waitGroup.Wrap(func() {
		controller.Run(c.stopChan)
	})
	return controller, nil
}

func (c *DiskController) onAdd(obj interface{}) {
	disk := obj.(*disktype.Disk)
	glog.Infof("[Custom Resouce Disk Controller] OnAdd %s\n", disk.ObjectMeta.SelfLink)

	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use diskScheme.Copy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	copyObj, err := c.DiskScheme.Copy(disk)
	if err != nil {
		glog.Errorf("ERROR creating a deep copy of disk object: %v\n", err)
		return
	}

	diskCopy := copyObj.(*disktype.Disk)

	// Add Cache
	c.Cache.AddDisk(diskCopy)
}

func (c *DiskController) onUpdate(oldObj, newObj interface{}) {
	oldDisk := oldObj.(*disktype.Disk)
	newDisk := newObj.(*disktype.Disk)
	glog.Infof("[Custom Resouce Disk Controller] OnUpdate oldObj: %s\n", oldDisk.ObjectMeta.SelfLink)
	glog.Infof("[Custom Resouce Disk Controller] OnUpdate newObj: %s\n", newDisk.ObjectMeta.SelfLink)

	// Update Cache
	c.Cache.UpdateDisk(newDisk)
}

func (c *DiskController) onDelete(obj interface{}) {
	disk := obj.(*disktype.Disk)
	glog.Infof("[Custom Resouce Disk Controller] OnDelete %s\n", disk.ObjectMeta.SelfLink)

	// Delete Cache
	c.Cache.DeleteDisk(disk)
}

// Controller Stop
func (c *DiskController) Stop() {
	close(c.stopChan)
	c.waitGroup.Wait()
}
