# kubelet组件pod资源获取详解

## 1. podConfig对象的初始化(k8s.io/kubernetes/pkg/kubelet/kubelet.go):

* 在(k8s.io/kubernetes/cmd/kubelet/app/server.go)中的CreateAndInitKubelet操作中调用NewMainKubelet
* podConfig对象的初始化makePodSourceConfig操作在NewMainKubelet操作中进行
* 在初始化podConfig对象时,然后根据配置信息如果配置有file的源,URL源或者apiserver源则向podConfig对象注册

```
// 初始化podConfig对象,该对象主要是从不同的源获取pod资源
func makePodSourceConfig(kubeCfg *componentconfig.KubeletConfiguration, kubeDeps *KubeletDeps, nodeName types.NodeName) (*config.PodConfig, error) {
	manifestURLHeader := make(http.Header)
	if kubeCfg.ManifestURLHeader != "" {
		pieces := strings.Split(kubeCfg.ManifestURLHeader, ":")
		if len(pieces) != 2 {
			return nil, fmt.Errorf("manifest-url-header must have a single ':' key-value separator, got %q", kubeCfg.ManifestURLHeader)
		}
		manifestURLHeader.Set(pieces[0], pieces[1])
	}

	// source of all configuration
	// config包初始化podConfig对象
	cfg := config.NewPodConfig(config.PodConfigNotificationIncremental, kubeDeps.Recorder)

	// define file config source
	if kubeCfg.PodManifestPath != "" {
		glog.Infof("Adding manifest file: %v", kubeCfg.PodManifestPath)
		// 从指定的文本文件中获取指定的pod资源
		config.NewSourceFile(kubeCfg.PodManifestPath, nodeName, kubeCfg.FileCheckFrequency.Duration, cfg.Channel(kubetypes.FileSource))
	}

	// define url config source
	if kubeCfg.ManifestURL != "" {
		glog.Infof("Adding manifest url %q with HTTP header %v", kubeCfg.ManifestURL, manifestURLHeader)
		// 根据指定的URL从指定服务获取pod资源
		config.NewSourceURL(kubeCfg.ManifestURL, manifestURLHeader, nodeName, kubeCfg.HTTPCheckFrequency.Duration, cfg.Channel(kubetypes.HTTPSource))
	}
	// 如果存在kubeclient对象,则处理从apiserver过来的pod源
	if kubeDeps.KubeClient != nil {
		glog.Infof("Watching apiserver")
		config.NewSourceApiserver(kubeDeps.KubeClient, nodeName, cfg.Channel(kubetypes.ApiserverSource))
	}
	return cfg, nil
}
```
## 2. podConfig对象的数据结构以及创建接口(k8s.io/kubernetes/pkg/kubelet/config):

* podConfig对象数据结构解析

```
// podConfig对象数据结构
type PodConfig struct {
	// 所有pod存储的对象
	pods *podStorage
	// mux包用来监听多个源的资源,当有资源的到达则会出发merge操作进行资源合并
	mux  *config.Mux

	// the channel of denormalized changes passed to listeners
	// 共用的pod资源更新channel,mux将合并后的pod资源发送到本channel中
	// syncLoop这个主groutine则监听这个channel来获取pod资源集合
	updates chan kubetypes.PodUpdate

	// contains the list of all configured sources
	// 源集合操作锁
	sourcesLock sync.Mutex
	// 所有的源集合
	sources     sets.String
}
```

* podConfig对象的初始化

```
// 初始化podConfig对象
func NewPodConfig(mode PodConfigNotificationMode, recorder record.EventRecorder) *PodConfig {
	updates := make(chan kubetypes.PodUpdate, 50)
	storage := newPodStorage(updates, mode, recorder)
	podConfig := &PodConfig{
		pods:    storage,
		mux:     config.NewMux(storage),
		updates: updates,
		sources: sets.String{},
	}
	return podConfig
}
```

* podStorage对象的初始化

```
// 初始化podStorage对象
func newPodStorage(updates chan<- kubetypes.PodUpdate, mode PodConfigNotificationMode, recorder record.EventRecorder) *podStorage {
	return &podStorage{
		pods:        make(map[string]map[types.UID]*v1.Pod),
		mode:        mode,
		updates:     updates,
		sourcesSeen: sets.String{},
		recorder:    recorder,
	}
}
```

## 3. podConfig对象中mux对象的作用以及让podConfig接收从apiserver的pod源(将从apiserver来的本节点的pod发送到podConfig对象中的podUpdate这个channel中)

* mux在创建podConfig的时候创建
* 不同的source注册的时候都会注册到mux中,当mux发现没有这种source,则创建一个groutine以及对该groutine的channel,
  源则将pod资源都发送到该channel中

```
// 让mux创建一个channel来接收从source源发送过来的pod资源
// 当mux创建的channel接收到pod数据后就会出发stroage的Merge操作
// 返回一个channel,源将产生的数据发送到这个channel上,这个channel对应的groutine监听到这个channel上面有数据
// 变化则会触发podStorage对象中的merge操作
func (c *PodConfig) Channel(source string) chan<- interface{} {
	c.sourcesLock.Lock()
	defer c.sourcesLock.Unlock()
	c.sources.Insert(source)
	return c.mux.Channel(source)
}
```

```
// 初始化apiserver的pod源
func NewSourceApiserver(c clientset.Interface, nodeName types.NodeName, updates chan<- interface{}) {
	// 根据kubeclient初始化listWatch对象,初始化中会过滤掉其他节点的pod只关心本节点上面的pod
	lw := cache.NewListWatchFromClient(c.Core().RESTClient(), "pods", metav1.NamespaceAll, fields.OneTermEqualSelector(api.PodHostField, string(nodeName)))
	newSourceApiserverFromLW(lw, updates)
}

// newSourceApiserverFromLW holds creates a config source that watches and pulls from the apiserver.
// 从apiserver接收到的本节点的pod发送到updates这个channel中
func newSourceApiserverFromLW(lw cache.ListerWatcher, updates chan<- interface{}) {
	// 将获得pod资源对象发送到目的地
	send := func(objs []interface{}) {
		var pods []*v1.Pod
		for _, o := range objs {
			pods = append(pods, o.(*v1.Pod))
		}
		updates <- kubetypes.PodUpdate{Pods: pods, Op: kubetypes.SET, Source: kubetypes.ApiserverSource}
	}
	cache.NewReflector(lw, &v1.Pod{}, cache.NewUndeltaStore(send, cache.MetaNamespaceKeyFunc), 0).Run()
}
```

## 4. mux对象里面如果发现源对应的channel上有任何数据的到来就会触发merge的操作:

* 当源对应的这些groutine发现有新的pod资源出现则会触发mux的merge合并动作,该动作将所有的资源合并后发送到
  podConfig对象中的updates这个channel中,syncLoop这个主groutine则监听这个updates channel来获取pod资源

```
// 回调Merge操作
func (s *podStorage) Merge(source string, change interface{}) error {
	s.updateLock.Lock()
	defer s.updateLock.Unlock()

	seenBefore := s.sourcesSeen.Has(source)
	// 获得增加，更新，删除，移除，只有状态改变的pod集合
	adds, updates, deletes, removes, reconciles := s.merge(source, change)
	firstSet := !seenBefore && s.sourcesSeen.Has(source)

	// deliver update notifications
	switch s.mode {
	case PodConfigNotificationIncremental:
		// 将移除的pod集合发送到updates这个pod统一channel中
		if len(removes.Pods) > 0 {
			s.updates <- *removes
		}
		// 将增加的pod集合发送到updates这个pod统一channel中
		if len(adds.Pods) > 0 {
			s.updates <- *adds
		}
		// 将更新的pod集合发送到updates这个pod统一channel中
		if len(updates.Pods) > 0 {
			s.updates <- *updates
		}
		// 将删除的pod集合发送到updates这个pod统一channel中
		if len(deletes.Pods) > 0 {
			s.updates <- *deletes
		}
		// kubelet第一次启动起来没有任何pod数据则向updates这个channel发送一个空的信息,这个信号表明kubelet pod资源的监听已经就绪
		if firstSet && len(adds.Pods) == 0 && len(updates.Pods) == 0 && len(deletes.Pods) == 0 {
			// Send an empty update when first seeing the source and there are
			// no ADD or UPDATE or DELETE pods from the source. This signals kubelet that
			// the source is ready.
			s.updates <- *adds
		}
		// Only add reconcile support here, because kubelet doesn't support Snapshot update now.
		// 将只改变状态的pod集合发送到updates这个pod统一channel中
		if len(reconciles.Pods) > 0 {
			s.updates <- *reconciles
		}

	case PodConfigNotificationSnapshotAndUpdates:
		if len(removes.Pods) > 0 || len(adds.Pods) > 0 || firstSet {
			s.updates <- kubetypes.PodUpdate{Pods: s.MergedState().([]*v1.Pod), Op: kubetypes.SET, Source: source}
		}
		if len(updates.Pods) > 0 {
			s.updates <- *updates
		}
		if len(deletes.Pods) > 0 {
			s.updates <- *deletes
		}

	case PodConfigNotificationSnapshot:
		if len(updates.Pods) > 0 || len(deletes.Pods) > 0 || len(adds.Pods) > 0 || len(removes.Pods) > 0 || firstSet {
			s.updates <- kubetypes.PodUpdate{Pods: s.MergedState().([]*v1.Pod), Op: kubetypes.SET, Source: source}
		}

	case PodConfigNotificationUnknown:
		fallthrough
	default:
		panic(fmt.Sprintf("unsupported PodConfigNotificationMode: %#v", s.mode))
	}

	return nil
}

func (s *podStorage) merge(source string, change interface{}) (adds, updates, deletes, removes, reconciles *kubetypes.PodUpdate) {
	s.podLock.Lock()
	defer s.podLock.Unlock()

	addPods := []*v1.Pod{}
	updatePods := []*v1.Pod{}
	deletePods := []*v1.Pod{}
	removePods := []*v1.Pod{}
	reconcilePods := []*v1.Pod{}

	pods := s.pods[source]
	if pods == nil {
		pods = make(map[types.UID]*v1.Pod)
	}

	// updatePodFunc is the local function which updates the pod cache *oldPods* with new pods *newPods*.
	// After updated, new pod will be stored in the pod cache *pods*.
	// Notice that *pods* and *oldPods* could be the same cache.
	// 增加,更新,删除pod处理函数
	updatePodsFunc := func(newPods []*v1.Pod, oldPods, pods map[types.UID]*v1.Pod) {
		filtered := filterInvalidPods(newPods, source, s.recorder)
		for _, ref := range filtered {
			// Annotate the pod with the source before any comparison.
			if ref.Annotations == nil {
				ref.Annotations = make(map[string]string)
			}
			ref.Annotations[kubetypes.ConfigSourceAnnotationKey] = source
			if existing, found := oldPods[ref.UID]; found {
				pods[ref.UID] = existing
				// 根据已经存在的pod和更新过来的pod进行对比返回是否是更新,删除,或者只是pod状态的变化
				needUpdate, needReconcile, needGracefulDelete := checkAndUpdatePod(existing, ref)
				if needUpdate {
					// 更新pod
					updatePods = append(updatePods, existing)
				} else if needReconcile {
					// pod状态变化的pod集合更新
					reconcilePods = append(reconcilePods, existing)
				} else if needGracefulDelete {
					// 删除pod操作
					deletePods = append(deletePods, existing)
				}
				continue
			}
			// 记录pod第一次被发现的时间点
			recordFirstSeenTime(ref)
			// 执行到这里就是新增pod
			pods[ref.UID] = ref
			addPods = append(addPods, ref)
		}
	}

	// 断言发送过来的资源为PodUpdate结构
	update := change.(kubetypes.PodUpdate)
	// The InitContainers and InitContainerStatuses fields are lost during
	// serialization and deserialization. They are conveyed via Annotations.
	// Setting these fields here so that kubelet doesn't have to check for
	// annotations.
	if source == kubetypes.ApiserverSource {
		for _, pod := range update.Pods {
			if err := podutil.SetInitContainersAndStatuses(pod); err != nil {
				glog.Error(err)
			}
		}
	}
	switch update.Op {
	// pod增加,更新,删除操作
	case kubetypes.ADD, kubetypes.UPDATE, kubetypes.DELETE:
		if update.Op == kubetypes.ADD {
			glog.V(4).Infof("Adding new pods from source %s : %v", source, update.Pods)
		} else if update.Op == kubetypes.DELETE {
			glog.V(4).Infof("Graceful deleting pods from source %s : %v", source, update.Pods)
		} else {
			glog.V(4).Infof("Updating pods from source %s : %v", source, update.Pods)
		}
		updatePodsFunc(update.Pods, pods, pods)

		// 移除pod
	case kubetypes.REMOVE:
		glog.V(4).Infof("Removing pods from source %s : %v", source, update.Pods)
		for _, value := range update.Pods {
			if existing, found := pods[value.UID]; found {
				// this is a delete
				delete(pods, value.UID)
				removePods = append(removePods, existing)
				continue
			}
			// this is a no-op
		}

		// pod操作集合,apiserver源就是返回这个类型
	case kubetypes.SET:
		glog.V(4).Infof("Setting pods for source %s", source)
		s.markSourceSet(source)
		// Clear the old map entries by just creating a new map
		oldPods := pods
		pods = make(map[types.UID]*v1.Pod)
		updatePodsFunc(update.Pods, oldPods, pods)
		for uid, existing := range oldPods {
			if _, found := pods[uid]; !found {
				// this is a delete
				removePods = append(removePods, existing)
			}
		}

	default:
		glog.Warningf("Received invalid update type: %v", update)

	}

	s.pods[source] = pods

	// 深度的将client里面的pod数据拷贝一份出来给node使用
	adds = &kubetypes.PodUpdate{Op: kubetypes.ADD, Pods: copyPods(addPods), Source: source}
	updates = &kubetypes.PodUpdate{Op: kubetypes.UPDATE, Pods: copyPods(updatePods), Source: source}
	deletes = &kubetypes.PodUpdate{Op: kubetypes.DELETE, Pods: copyPods(deletePods), Source: source}
	removes = &kubetypes.PodUpdate{Op: kubetypes.REMOVE, Pods: copyPods(removePods), Source: source}
	reconciles = &kubetypes.PodUpdate{Op: kubetypes.RECONCILE, Pods: copyPods(reconcilePods), Source: source}

	return adds, updates, deletes, removes, reconciles
}
```

## 5. 初始化完成后的podConfig对象在kubelet对象的startKubelet操作中开始使用

* 在startKubelet操作中启动kubelet的Run协程,传入的参数即是podConfig对象中的podUpdate对象channel(传入的参数即是podConfig对象中pod资源变化的统一channel updates)

```
func startKubelet(k kubelet.KubeletBootstrap, podCfg *config.PodConfig, kubeCfg *componentconfig.KubeletConfiguration, kubeDeps *kubelet.KubeletDeps) {
	// start the kubelet
	go wait.Until(func() { k.Run(podCfg.Updates()) }, 0, wait.NeverStop)
}
```

* 在kubelet的Run操作中,调用syncLoop操作:

```
// kubelet对象的运行
func (kl *Kubelet) Run(updates <-chan kubetypes.PodUpdate) {
    ...
    
    // 该updates即是podConfig对象中podUpdate对象的channel
    kl.syncLoop(updates, kl)
}
```

## 6. syncLoop操作就是循环不断从podUpdate这个channel中获取更新的pod资源进行处理:

```
// syncLoop是主要的处理变化的loop
func (kl *Kubelet) syncLoop(updates <-chan kubetypes.PodUpdate, handler SyncHandler) {
	glog.Info("Starting kubelet main sync loop.")
	// The resyncTicker wakes up kubelet to checks if there are any pod workers
	// that need to be sync'd. A one-second period is sufficient because the
	// sync interval is defaulted to 10s.
	syncTicker := time.NewTicker(time.Second)
	defer syncTicker.Stop()
	housekeepingTicker := time.NewTicker(housekeepingPeriod)
	defer housekeepingTicker.Stop()
	plegCh := kl.pleg.Watch()
	for {
		if rs := kl.runtimeState.runtimeErrors(); len(rs) != 0 {
			glog.Infof("skipping pod synchronization - %v", rs)
			time.Sleep(5 * time.Second)
			continue
		}

		kl.syncLoopMonitor.Store(kl.clock.Now())
		if !kl.syncLoopIteration(updates, handler, syncTicker.C, housekeepingTicker.C, plegCh) {
			break
		}
		kl.syncLoopMonitor.Store(kl.clock.Now())
	}
}
```

## 7. 最终处理从统一channel上获取的pod资源变化:

```
// 从各种channel上读取数据,然后分发给传入的handler进行处理
func (kl *Kubelet) syncLoopIteration(configCh <-chan kubetypes.PodUpdate, handler SyncHandler,
	syncCh <-chan time.Time, housekeepingCh <-chan time.Time, plegCh <-chan *pleg.PodLifecycleEvent) bool {
	select {
	// 从podConfig对象中的updates这个channel上获取pod变化的资源数据
	case u, open := <-configCh:
		// Update from a config source; dispatch it to the right handler
		// callback.
		if !open {
			glog.Errorf("Update channel is closed. Exiting the sync loop.")
			return false
		}

		switch u.Op {
		// 处理pod的增加
		case kubetypes.ADD:
			glog.V(2).Infof("SyncLoop (ADD, %q): %q", u.Source, format.Pods(u.Pods))
			// After restarting, kubelet will get all existing pods through
			// ADD as if they are new pods. These pods will then go through the
			// admission process and *may* be rejected. This can be resolved
			// once we have checkpointing.
			handler.HandlePodAdditions(u.Pods)
			// 处理pod的更新操作
		case kubetypes.UPDATE:
			glog.V(2).Infof("SyncLoop (UPDATE, %q): %q", u.Source, format.PodsWithDeletiontimestamps(u.Pods))
			handler.HandlePodUpdates(u.Pods)
			// 处理pod的移除
		case kubetypes.REMOVE:
			glog.V(2).Infof("SyncLoop (REMOVE, %q): %q", u.Source, format.Pods(u.Pods))
			handler.HandlePodRemoves(u.Pods)
			// 处理只有状态变化的pod集合
		case kubetypes.RECONCILE:
			glog.V(4).Infof("SyncLoop (RECONCILE, %q): %q", u.Source, format.Pods(u.Pods))
			handler.HandlePodReconcile(u.Pods)
			// 处理pod的删除操作
		case kubetypes.DELETE:
			glog.V(2).Infof("SyncLoop (DELETE, %q): %q", u.Source, format.Pods(u.Pods))
			// DELETE is treated as a UPDATE because of graceful deletion.
			handler.HandlePodUpdates(u.Pods)
			// 处理pod集合(目前是不支持的)
		case kubetypes.SET:
			// TODO: Do we want to support this?
			glog.Errorf("Kubelet does not support snapshot update")
		}

		// Mark the source ready after receiving at least one update from the
		// source. Once all the sources are marked ready, various cleanup
		// routines will start reclaiming resources. It is important that this
		// takes place only after kubelet calls the update handler to process
		// the update to ensure the internal pod cache is up-to-date.
		kl.sourcesReady.AddSource(u.Source)
	case e := <-plegCh:
		if isSyncPodWorthy(e) {
			// PLEG event for a pod; sync it.
			if pod, ok := kl.podManager.GetPodByUID(e.ID); ok {
				glog.V(2).Infof("SyncLoop (PLEG): %q, event: %#v", format.Pod(pod), e)
				handler.HandlePodSyncs([]*v1.Pod{pod})
			} else {
				// If the pod no longer exists, ignore the event.
				glog.V(4).Infof("SyncLoop (PLEG): ignore irrelevant event: %#v", e)
			}
		}

		if e.Type == pleg.ContainerDied {
			if containerID, ok := e.Data.(string); ok {
				kl.cleanUpContainersInPod(e.ID, containerID)
			}
		}
	case <-syncCh:
		// Sync pods waiting for sync
		podsToSync := kl.getPodsToSync()
		if len(podsToSync) == 0 {
			break
		}
		glog.V(4).Infof("SyncLoop (SYNC): %d pods; %s", len(podsToSync), format.Pods(podsToSync))
		kl.HandlePodSyncs(podsToSync)
	case update := <-kl.livenessManager.Updates():
		if update.Result == proberesults.Failure {
			// The liveness manager detected a failure; sync the pod.

			// We should not use the pod from livenessManager, because it is never updated after
			// initialization.
			pod, ok := kl.podManager.GetPodByUID(update.PodUID)
			if !ok {
				// If the pod no longer exists, ignore the update.
				glog.V(4).Infof("SyncLoop (container unhealthy): ignore irrelevant update: %#v", update)
				break
			}
			glog.V(1).Infof("SyncLoop (container unhealthy): %q", format.Pod(pod))
			handler.HandlePodSyncs([]*v1.Pod{pod})
		}
	case <-housekeepingCh:
		if !kl.sourcesReady.AllReady() {
			// If the sources aren't ready or volume manager has not yet synced the states,
			// skip housekeeping, as we may accidentally delete pods from unready sources.
			glog.V(4).Infof("SyncLoop (housekeeping, skipped): sources aren't ready yet.")
		} else {
			glog.V(4).Infof("SyncLoop (housekeeping)")
			if err := handler.HandlePodCleanups(); err != nil {
				glog.Errorf("Failed cleaning pods: %v", err)
			}
		}
	}
	return true
}
```