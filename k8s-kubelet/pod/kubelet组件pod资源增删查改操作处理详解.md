# kubelet组件pod资源增删查改操作处理详解

## 1. pod增加处理入口(k8s.io/kubernetes/pkg/kubelet/kubelet.go)HandlePodAdditions:

```
// 处理本节点上面pod的增加
func (kl *Kubelet) HandlePodAdditions(pods []*v1.Pod) {
	start := kl.clock.Now()
	// 先根据pod的产生时间进行排序
	sort.Sort(sliceutils.PodsByCreationTime(pods))
	// 遍历新增的pod资源列表进行处理
	for _, pod := range pods {
		// 获取当前节点已经存在的所有pod资源集合
		existingPods := kl.podManager.GetPods()
		// Always add the pod to the pod manager. Kubelet relies on the pod
		// manager as the source of truth for the desired state. If a pod does
		// not exist in the pod manager, it means that it has been deleted in
		// the apiserver and no action (other than cleanup) is required.
		// 向pod管理器注册新增的pod资源
		kl.podManager.AddPod(pod)

		// 如果发现是
		if kubepod.IsMirrorPod(pod) {
			// 根据mirror pod从pod管理器里面拿到对应的static pod,然后进行分发操作
			kl.handleMirrorPod(pod, start)
			continue
		}

		if !kl.podIsTerminated(pod) {
			// Only go through the admission process if the pod is not
			// terminated.

			// We failed pods that we rejected, so activePods include all admitted
			// pods that are alive.
			activePods := kl.filterOutTerminatedPods(existingPods)

			// Check if we can admit the pod; if not, reject it.
			if ok, reason, message := kl.canAdmitPod(activePods, pod); !ok {
				kl.rejectPod(pod, reason, message)
				continue
			}
		}
		// 根据正常pod获取mirror类型的pod
		mirrorPod, _ := kl.podManager.GetMirrorPodByPod(pod)
		// 然后进行分发工作
		kl.dispatchWork(pod, kubetypes.SyncPodCreate, mirrorPod, start)
		// 将pod添加到探活管理器中进行探活相关操作
		kl.probeManager.AddPod(pod)
	}
}
```