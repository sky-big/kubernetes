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

package discovery

import (
	"fmt"
	"hash/fnv"
	"path/filepath"

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/external-storage/local-volume/provisioner/pkg/common"

	v1helper "github.com/kubernetes-incubator/external-storage/lib/helper"
	"k8s.io/client-go/pkg/api/v1"
)

// Discoverer finds available volumes and creates PVs for them
// It looks for volumes in the directories specified in the discoveryMap
type Discoverer struct {
	*common.RuntimeConfig
	nodeAffinityAnn string
}

// NewDiscoverer creates a Discoverer object that will scan through
// the configured directories and create local PVs for any new directories found
func NewDiscoverer(config *common.RuntimeConfig) (*Discoverer, error) {
	affinity, err := generateNodeAffinity(config.Node)
	if err != nil {
		return nil, fmt.Errorf("Failed to generate node affinity: %v", err)
	}
	tmpAnnotations := map[string]string{}
	err = v1helper.StorageNodeAffinityToAlphaAnnotation(tmpAnnotations, affinity)
	if err != nil {
		return nil, fmt.Errorf("Failed to convert node affinity to alpha annotation: %v", err)
	}
	return &Discoverer{RuntimeConfig: config, nodeAffinityAnn: tmpAnnotations[v1.AlphaStorageNodeAffinityAnnotation]}, nil
}

func generateNodeAffinity(node *v1.Node) (*v1.NodeAffinity, error) {
	if node.Labels == nil {
		return nil, fmt.Errorf("Node does not have labels")
	}
	nodeValue, found := node.Labels[common.NodeLabelKey]
	if !found {
		return nil, fmt.Errorf("Node does not have expected label %s", common.NodeLabelKey)
	}

	return &v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{
				{
					MatchExpressions: []v1.NodeSelectorRequirement{
						{
							Key:      common.NodeLabelKey,
							Operator: v1.NodeSelectorOpIn,
							Values:   []string{nodeValue},
						},
					},
				},
			},
		},
	}, nil
}

// DiscoverLocalVolumes reads the configured discovery paths, and creates PVs for the new volumes
func (d *Discoverer) DiscoverLocalVolumes() {
	for class, config := range d.DiscoveryMap {
		d.discoverVolumesAtPath(class, config)
	}
}

func (d *Discoverer) discoverVolumesAtPath(class string, config common.MountConfig) {
	glog.V(7).Infof("Discovering volumes at hostpath %q, mount path %q for storage class %q", config.HostDir, config.MountDir, class)

	files, err := d.VolUtil.ReadDir(config.MountDir)
	if err != nil {
		glog.Errorf("Error reading directory: %v", err)
		return
	}

	for _, file := range files {
		// Check if PV already exists for it
		pvName := generatePVName(file, d.Node.Name, class)
		_, exists := d.Cache.GetPV(pvName)
		if exists {
			continue
		}
		filePath := filepath.Join(config.MountDir, file)
		err = d.validateFile(filePath)
		if err != nil {
			glog.Errorf("Mount path %q validation failed: %v", filePath, err)
			continue
		}
		availByte, err := d.VolUtil.GetFsAvailableByte(filePath)
		if err != nil {
			glog.Errorf("Path %q fs stats error: %v", filePath, err)
			continue
		}
		d.createPV(file, class, config, availByte)
	}
}

func (d *Discoverer) validateFile(fullPath string) error {
	isDir, err := d.VolUtil.IsDir(fullPath)
	if err != nil {
		return fmt.Errorf("Error getting path info: %v", err)
	}
	if !isDir {
		return fmt.Errorf("Path is not a directory")
	}
	return nil
}

func generatePVName(file, node, class string) string {
	h := fnv.New32a()
	h.Write([]byte(file))
	h.Write([]byte(node))
	h.Write([]byte(class))
	// This is the FNV-1a 32-bit hash
	return fmt.Sprintf("local-pv-%x", h.Sum32())
}

func (d *Discoverer) createPV(file, class string, config common.MountConfig, availByte uint64) {
	pvName := generatePVName(file, d.Node.Name, class)
	outsidePath := filepath.Join(config.HostDir, file)

	glog.Infof("Found new volume at host path %q with capacity %d, creating Local PV %q", outsidePath, availByte, pvName)
	pvSpec := common.CreateLocalPVSpec(&common.LocalPVConfig{
		Name:            pvName,
		HostPath:        outsidePath,
		Capacity:        availByte,
		StorageClass:    class,
		ProvisionerName: d.Name,
		AffinityAnn:     d.nodeAffinityAnn,
	})

	_, err := d.APIUtil.CreatePV(pvSpec)
	if err != nil {
		glog.Errorf("Error creating PV %q for volume at %q: %v", pvName, outsidePath, err)
		return
	}
	glog.Infof("Created PV %q for volume at %q", pvName, outsidePath)
}
