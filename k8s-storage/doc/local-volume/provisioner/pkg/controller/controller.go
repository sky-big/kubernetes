/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	"github.com/kubernetes-incubator/external-storage/local-volume/provisioner/pkg/cache"
	"github.com/kubernetes-incubator/external-storage/local-volume/provisioner/pkg/common"
	"github.com/kubernetes-incubator/external-storage/local-volume/provisioner/pkg/deleter"
	"github.com/kubernetes-incubator/external-storage/local-volume/provisioner/pkg/discovery"
	"github.com/kubernetes-incubator/external-storage/local-volume/provisioner/pkg/populator"
	"github.com/kubernetes-incubator/external-storage/local-volume/provisioner/pkg/util"

	"k8s.io/client-go/kubernetes"
)

// StartLocalController starts the sync loop for the local PV discovery and deleter
func StartLocalController(client *kubernetes.Clientset, config *common.UserConfig) {
	glog.Info("Initializing volume cache\n")

	runtimeConfig := &common.RuntimeConfig{
		UserConfig: config,
		Cache:      cache.NewVolumeCache(),
		VolUtil:    util.NewVolumeUtil(),
		APIUtil:    util.NewAPIUtil(client),
		Client:     client,
		Name:       fmt.Sprintf("local-volume-provisioner-%v-%v", config.Node.Name, config.Node.UID),
	}

	populator := populator.NewPopulator(runtimeConfig)
	populator.Start()

	discoverer, err := discovery.NewDiscoverer(runtimeConfig)
	if err != nil {
		glog.Fatalf("Error starting discoverer: %v", err)
	}

	deleter := deleter.NewDeleter(runtimeConfig)

	glog.Info("Controller started\n")
	for {
		deleter.DeletePVs()
		discoverer.DiscoverLocalVolumes()
		time.Sleep(10 * time.Second)
	}
}
