package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const DiskResourcePlural = "disks"

type Disk struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              DiskSpec   `json:"spec"`
	Status            DiskStatus `json:"status,omitempty"`
}

type DiskSpec struct {
	Node          string `json:"node"`
	Path          string `json:"path"`
	Type          string `json:"type"`
	Capacity      int64  `json:"capacity"`
	ApplyCapacity int64  `json:"applycapacity"`
	IOPS          int64  `json:"iops"`
}

type DiskStatus struct {
	State DiskState `json:"state,omitempty"`
}

type DiskState string

const (
	DiskStateRunning DiskState = "Created"
)

type DiskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Disk `json:"items"`
}
