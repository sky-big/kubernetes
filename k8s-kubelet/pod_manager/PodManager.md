# pod manager(kubelet节点上所有pod的管理器)

## 1. pod管理器对象数据结构(k8s.io/kubernetes/pkg/kubelet/pod/pod_manager.go):

* 从URL和File获得的Pod在本地除了创建正常的pod之外还会存在一个本地镜像pod

```
// pod管理器数据结构
// mirror pod是kubelet本地的静态颇大启动后向ApiServer启动的一个pod资源,
// 启动mirror pod到ApiServer主要是为了方便用户从ApiServer去查看所有的pod,
// 但是不能从ApiServer去删除这个mirror pod,只能到对应的kubelet节点上面删除容器
type basicManager struct {
	// Protects all internal maps.
	// 保护所有内部的map数据
	lock sync.RWMutex

	// Regular pods indexed by UID.
	// 当前节点上所有pod的UID对应的pod对象指针的集合
	podByUID map[types.UID]*v1.Pod
	// Mirror pods indexed by UID.
	// 当前节点上所有本地静态pod的UID对应的pod对象指针的集合
	mirrorPodByUID map[types.UID]*v1.Pod

	// Pods indexed by full name for easy access.
	// 当前节点上所有pod的名字对应的pod对象指针的集合
	podByFullName       map[string]*v1.Pod
	// 当前节点上所有本地静态pod的名字对应的pod对象指针的集合
	mirrorPodByFullName map[string]*v1.Pod

	// Mirror pod UID to pod UID map.
	// 当前节点上所有本地静态pod的UID对应正常pod的UID的集合
	translationByUID map[types.UID]types.UID

	// basicManager is keeping secretManager and configMapManager up-to-date.
	// secret管理器对象
	secretManager    secret.Manager
	// configMap管理器对象
	configMapManager configmap.Manager

	// A mirror pod client to create/delete mirror pods.
	// 本地静态pod的客户端,用来创建删除本地静态pod
	MirrorClient
}
```

## 2. pod管理器的初始化:

```
// 初始化pod管理器对象
func NewBasicPodManager(client MirrorClient, secretManager secret.Manager, configMapManager configmap.Manager) Manager {
	pm := &basicManager{}
	pm.secretManager = secretManager
	pm.configMapManager = configMapManager
	pm.MirrorClient = client
	pm.SetPods(nil)
	return pm
}

// pod管理器第一次创建的时候调用来初始化所有类型的pod集合,同时根据传入的pod集合进行内部更新
func (pm *basicManager) SetPods(newPods []*v1.Pod) {
	pm.lock.Lock()
	defer pm.lock.Unlock()

	pm.podByUID = make(map[types.UID]*v1.Pod)
	pm.podByFullName = make(map[string]*v1.Pod)
	pm.mirrorPodByUID = make(map[types.UID]*v1.Pod)
	pm.mirrorPodByFullName = make(map[string]*v1.Pod)
	pm.translationByUID = make(map[types.UID]types.UID)

	pm.updatePodsInternal(newPods...)
}
```

## 3. pod管理器的增删查改操作:

```
// 添加pod的接口
func (pm *basicManager) AddPod(pod *v1.Pod) {
	pm.UpdatePod(pod)
}

// 更新某个pod的接口
func (pm *basicManager) UpdatePod(pod *v1.Pod) {
	pm.lock.Lock()
	defer pm.lock.Unlock()
	pm.updatePodsInternal(pod)
}

// 将pod集合添加到pod管理器对象中,同时注册到secret,configMap管理器中
func (pm *basicManager) updatePodsInternal(pods ...*v1.Pod) {
	for _, pod := range pods {
		// 向secrect管理器对象进行注册
		if pm.secretManager != nil {
			// TODO: Consider detecting only status update and in such case do
			// not register pod, as it doesn't really matter.
			pm.secretManager.RegisterPod(pod)
		}
		// 向configMap管理器对象进行注册
		if pm.configMapManager != nil {
			// TODO: Consider detecting only status update and in such case do
			// not register pod, as it doesn't really matter.
			pm.configMapManager.RegisterPod(pod)
		}
		// pod的全名字组合是通过pod的名字加下划线再加pod的namespace
		podFullName := kubecontainer.GetPodFullName(pod)
		// 如果是本地镜像pod则添加到本节点静态pod的集合中
		if IsMirrorPod(pod) {
			pm.mirrorPodByUID[pod.UID] = pod
			pm.mirrorPodByFullName[podFullName] = pod
			if p, ok := pm.podByFullName[podFullName]; ok {
				pm.translationByUID[pod.UID] = p.UID
			}
			// 如果不是本地镜像pod,则添加到pod管理器对象中正常pod的集合中
		} else {
			pm.podByUID[pod.UID] = pod
			pm.podByFullName[podFullName] = pod
			if mirror, ok := pm.mirrorPodByFullName[podFullName]; ok {
				pm.translationByUID[mirror.UID] = pod.UID
			}
		}
	}
}

// 删除pod的接口
func (pm *basicManager) DeletePod(pod *v1.Pod) {
	pm.lock.Lock()
	defer pm.lock.Unlock()
	// 向secret管理器进行取消注册
	if pm.secretManager != nil {
		pm.secretManager.UnregisterPod(pod)
	}
	// 向configMap管理器进行取消注册
	if pm.configMapManager != nil {
		pm.configMapManager.UnregisterPod(pod)
	}
	podFullName := kubecontainer.GetPodFullName(pod)
	// 如果是本地镜像pod则从本地静态pod集合中删除
	if IsMirrorPod(pod) {
		delete(pm.mirrorPodByUID, pod.UID)
		delete(pm.mirrorPodByFullName, podFullName)
		delete(pm.translationByUID, pod.UID)
		// 如果非本地镜像pod则从正常的pod集合中删除pod
	} else {
		delete(pm.podByUID, pod.UID)
		delete(pm.podByFullName, podFullName)
	}
}

// 获取当前节点的所有pod集合
func (pm *basicManager) GetPods() []*v1.Pod {
	pm.lock.RLock()
	defer pm.lock.RUnlock()
	return podsMapToPods(pm.podByUID)
}

// 将map集合的pod转化为pod集合的切片
func podsMapToPods(UIDMap map[types.UID]*v1.Pod) []*v1.Pod {
	pods := make([]*v1.Pod, 0, len(UIDMap))
	for _, pod := range UIDMap {
		pods = append(pods, pod)
	}
	return pods
}
```