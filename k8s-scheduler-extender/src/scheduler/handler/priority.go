package handler

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	disktype "resource/disk/types"

	"github.com/golang/glog"
	"github.com/julienschmidt/httprouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	clientv1 "k8s.io/client-go/pkg/api/v1"
	apiv1 "k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/plugin/pkg/scheduler/api/v1"
)

func (h *Handler) StorageSchedulerPriority(w http.ResponseWriter, req *http.Request, ps httprouter.Params) error {
	body, err := ioutil.ReadAll(req.Body)
	defer req.Body.Close()
	if err != nil {
		return err
	}

	// parse scheduler extender send extender args
	var extenderArgs v1.ExtenderArgs
	if err := json.Unmarshal(body, &extenderArgs); err != nil {
		sendEmptyPriorityResult(w)
		return err
	}
	glog.Infof("StorageSchedulerPriority Receieve Scheduler Extender Args : %v", extenderArgs.NodeNames)

	// update pod mount path
	err = h.updatePodPath(&extenderArgs.Pod)
	if err != nil {
		glog.Infof("update pod path error : %v", err)
		// backup empty result
		sendEmptyPriorityResult(w)
		return err
	}

	// select optimal disk
	resultDisk := h.selectDisk(*extenderArgs.NodeNames)
	glog.Infof("select disk : %v", resultDisk)
	if resultDisk == nil {
		errorStr := "scheduler priority not select disk"
		glog.Infof(errorStr)
		// backup empty result
		sendEmptyPriorityResult(w)
		return err
	}

	// make selected node
	var result v1.HostPriorityList
	oneResult := v1.HostPriority{
		Host:  resultDisk.Spec.Node,
		Score: 10,
	}
	result = append(result, oneResult)

	// send result
	out, err := json.Marshal(result)
	if err != nil {
		return err
	}
	w.Write(out)

	return nil
}

// select disk
func (h *Handler) selectDisk(nodeNames []string) *disktype.Disk {
	controllerMgr := h.controllerMgr

	// first get all disks resource by node name list
	disks := []*disktype.Disk{}
	for _, nodeName := range nodeNames {
		disks = append(disks, controllerMgr.DiskController.Cache.GetDisksByNode(nodeName)...)
	}

	// select max capacity disk
	return selectOptimalDisk(disks)
}

// select max enable capacity disk
func selectOptimalDisk(disks []*disktype.Disk) *disktype.Disk {
	var result *disktype.Disk
	var maxCapacity int64

	for _, disk := range disks {
		curMaxCapacity := disk.Spec.Capacity - disk.Spec.ApplyCapacity
		if curMaxCapacity > maxCapacity {
			maxCapacity = curMaxCapacity
			result = disk
		}
	}

	return result
}

// update pod mount path
func (h *Handler) updatePodPath(pod *apiv1.Pod) error {
	podbin, err := json.Marshal(pod)
	if err != nil {
		return err
	}
	var clientpod clientv1.Pod
	err = json.Unmarshal(podbin, &clientpod)
	if err != nil {
		return err
	}
	if clientpod.ObjectMeta.Name == "busybox" {
		clientpod.Spec.Volumes[0].EmptyDir.Path = "/mnt/test/" + clientpod.Name
	}
	clientpodbin, err := json.Marshal(clientpod)
	if err != nil {
		return err
	}
	for {
		_, err = h.client.Core().Pods(metav1.NamespaceDefault).Patch(clientpod.Name, types.MergePatchType, clientpodbin)
		if err == nil {
			break
		}
	}

	return nil
}

func sendEmptyPriorityResult(w http.ResponseWriter) {
	var result v1.HostPriorityList
	out, _ := json.Marshal(result)
	w.Write(out)
}
