package controller

import (
	diskcontroller "resource/disk/controller"
	"scheduler/configure"
	"util"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"

	"github.com/golang/glog"
)

type ControllerManager struct {
	ExtensionsClient apiextensionsclient.Interface
	DiskController   *diskcontroller.DiskController
}

func NewControllerManager() (*ControllerManager, error) {
	// extensions client
	extensionsClient, err := util.CreateApiExtensionsClient(configure.Conf.KubeConfig)
	if err != nil {
		glog.Fatalf("Error creating extensions client: %v\n", err)
		return nil, err
	}

	// init disk controller
	diskController, err := diskcontroller.NewDiskController(extensionsClient, configure.Conf.KubeConfig)
	if err != nil {
		glog.Error(err)
		return nil, err
	}

	return &ControllerManager{
		ExtensionsClient: extensionsClient,
		DiskController:   diskController,
	}, nil
}

// Run
func (s *ControllerManager) Run() error {
	// run disk controller
	if err := s.DiskController.Run(); err != nil {
		glog.Error(err)
		return err
	}

	glog.Infof("Starting Custom Resource Controller Manager")

	return nil
}

// Stop
func (s *ControllerManager) Stop() {
	// stop disk controller
	s.DiskController.Stop()
}
