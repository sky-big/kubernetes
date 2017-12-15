package handler

import (
	"scheduler/configure"
	"scheduler/controller"
	"util"

	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
)

type Handler struct {
	client        *kubernetes.Clientset
	controllerMgr *controller.ControllerManager
}

// New
func NewHandler() (*Handler, error) {
	// Create the client config. Use kubeconfig if given, otherwise assume in-cluster.
	config, err := util.BuildConfig(configure.Conf.KubeConfig)
	if err != nil {
		glog.Error(err)
		return nil, err
	}

	// new client
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Error creating client: %v\n", err)
		return nil, err
	}

	// new controller manager
	controllerMgr, err := controller.NewControllerManager()
	if err != nil {
		glog.Error(err)
		return nil, err
	}

	return &Handler{
		client:        client,
		controllerMgr: controllerMgr,
	}, nil
}

// Run
func (h *Handler) Run() error {
	if err := h.controllerMgr.Run(); err != nil {
		glog.Error(err)
		return err
	}

	return nil
}
