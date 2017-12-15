# PodWorkers详解

## 0. PodWorkers功能详解：

* 为每一个pod启动一个对应的groutine，该groutine用来处理每个pod的更新消息，包括创建删除更新，在创建groutine的同时会创建一个channel用来外部向groutine发送消息，PodWorkers里面记录了每个pod对应的这个通信channel
* 每次主groutine都会将每个pod的更新消息通过UpdatePod接口来找到pod对应的channel,然后把更新消息发送到该channel上面，让pod对应的groutine去进行处理更新消息
* 每个pod在处理更新消息的时候都是在podWorkers里面记录为工作状态中，当在工作状态中的时候如果来了更新消息则将该更新消息覆盖缓存到podWorkers中，因为每次更新消息传递过来的都是pod指针因此更新消息时可以覆盖的，但是删除消息则是不能覆盖的，让删除消息肯定被执行

## 1. PodWorkers的数据结构以及初始化操作(k8s.io/kubernetes/pkg/kubelet/pod_workers.go):

* PodWorkers数据结构详解：
```
// podWorkers数据结构,里面管理了所有pod groutine进行通信的channel
type podWorkers struct {
    // Protects all per worker fields.
    podLock sync.Mutex

    // Tracks all running per-pod goroutines - per-pod goroutine will be
    // processing updates received through its corresponding channel.
    // 主groutine跟每个pod对应的groutine进行联系的channel集合
    podUpdates map[types.UID]chan UpdatePodOptions
    // Track the current state of per-pod goroutines.
    // Currently all update request for a given pod coming when another
    // update of this pod is being processed are ignored.
    // 表示所有pod是否正在处理更新消息的工作状态中
    isWorking map[types.UID]bool
    // Tracks the last undelivered work item for this pod - a work item is
    // undelivered if it comes in while the worker is working.
    // 如果一个更新消息过来发现groutine正在处理更新消息的工作中,需要把这次的更新消息缓存到此处
    // 但是每次更新传入的都是pod指针,因此此处的更新消息是可以覆盖的，不影响更新操作，但是不能覆盖掉删除消息
    lastUndeliveredWorkUpdate map[types.UID]UpdatePodOptions

    workQueue queue.WorkQueue

    // This function is run to sync the desired stated of pod.
    // NOTE: This function has to be thread-safe - it can be called for
    // different pods at the same time.
    // 每个pod进行更新操作执行的操作,该操作函数可以同时被不同的pod在相同的时间点执行
    syncPodFn syncPodFnType

    // The EventRecorder to use
    recorder record.EventRecorder

    // backOffPeriod is the duration to back off when there is a sync error.
    backOffPeriod time.Duration

    // resyncInterval is the duration to wait until the next sync.
    resyncInterval time.Duration

    // podCache stores kubecontainer.PodStatus for all pods.
    podCache kubecontainer.Cache
}
```

* PodWorkers初始化操作：
```
// 初始化podWorkers对象
func newPodWorkers(syncPodFn syncPodFnType, recorder record.EventRecorder, workQueue queue.WorkQueue,
    resyncInterval, backOffPeriod time.Duration, podCache kubecontainer.Cache) *podWorkers {
    return &podWorkers{
        podUpdates:                map[types.UID]chan UpdatePodOptions{},
        isWorking:                 map[types.UID]bool{},
        lastUndeliveredWorkUpdate: map[types.UID]UpdatePodOptions{},
        syncPodFn:                 syncPodFn,
        recorder:                  recorder,
        workQueue:                 workQueue,
        resyncInterval:            resyncInterval,
        backOffPeriod:             backOffPeriod,
        podCache:                  podCache,
    }
}
```

## 2. PodWorkers暴露出的更新接口：

* 该接口会由主groutine在有pod更新过来的时候会调用过来
* 每个pod在此处会给该pod创建一个groutine以及对应的channel，channel是外部的groutine向每个pod对应的groutine发送数据的入口
* 在该更新接口里面,如果发现pod在podWorkers里面不存在对应的channel,表明是新创建的pod,因此会给pod创建对应的groutine以及该groutine对应的channel
* 如果发现该pod对应的groutine正在处理更新消息的工作状态中,会把更新消息覆盖式的存储在podWorkers结构的lastUndeliveredWorkUpdate字段中,在groutine处理完毕上一个更新消息后，会从此字段中拿到更新消息继续进行更新动作

```
// pod更新入口
func (p *podWorkers) UpdatePod(options *UpdatePodOptions) {
    pod := options.Pod
    uid := pod.UID
    var podUpdates chan UpdatePodOptions
    var exists bool

    p.podLock.Lock()
    defer p.podLock.Unlock()
    // 如果该pod对应的更新channel不存在则说明是新的pod,则创建该pod对应的groutine以及channel
    if podUpdates, exists = p.podUpdates[uid]; !exists {
        // We need to have a buffer here, because checkForUpdates() method that
        // puts an update into channel is called from the same goroutine where
        // the channel is consumed. However, it is guaranteed that in such case
        // the channel is empty, so buffer of size 1 is enough.
        // 创建的channel缓存长度为1,即默认向该channel发送一个消息后不会阻塞
        podUpdates = make(chan UpdatePodOptions, 1)
        p.podUpdates[uid] = podUpdates

        // Creating a new pod worker either means this is a new pod, or that the
        // kubelet just restarted. In either case the kubelet is willing to believe
        // the status of the pod for the first pod worker sync. See corresponding
        // comment in syncPod.
        // 为每个新的pod启动一个groutine去循环阻塞执行managePodLoop操作
        // 该groutine一直循环阻塞等待对应pod的更新消息的到来
        go func() {
            defer runtime.HandleCrash()
            p.managePodLoop(podUpdates)
        }()
    }
    // 如果当前groutine没有正在处理更新消息,则将更新消息发送到该groutine对应的channel上
    // 同时将当前groutine设置为正在工作状态
    if !p.isWorking[pod.UID] {
        p.isWorking[pod.UID] = true
        podUpdates <- *options
        // 如果当前pod对应的groutine正在工作,则将该更新消息存储起来,等待groutine将上一条更新消息处理完毕后
        // 再来处理着一条更新消息
    } else {
        // if a request to kill a pod is pending, we do not let anything overwrite that request.
        // 死亡的更新消息不能进行覆盖
        update, found := p.lastUndeliveredWorkUpdate[pod.UID]
        if !found || update.UpdateType != kubetypes.SyncPodKill {
            p.lastUndeliveredWorkUpdate[pod.UID] = *options
        }
    }
}
```

## 3.每个pod对应的groutine执行的操作

* 如果发现更新的消息比缓存中的pod状态新则进行处理更新消息
* 处理完更新消息后如果发现有更新完毕的回调函数则进行回调
* 去查看是否有等待该groutine处理更新消息，如果不存在则将groutine设置为空闲等待状态，否则继续去处理等待的更新消息，直到没有等待的更新消息

```
// 每个pod对应的groutine,循环阻塞从更新的channel上获取更新消息
func (p *podWorkers) managePodLoop(podUpdates <-chan UpdatePodOptions) {
    var lastSyncTime time.Time
    // 循环阻塞收取更新消息
    for update := range podUpdates {
        err := func() error {
            podUID := update.Pod.UID
            // This is a blocking call that would return only if the cache
            // has an entry for the pod that is newer than minRuntimeCache
            // Time. This ensures the worker doesn't start syncing until
            // after the cache is at least newer than the finished time of
            // the previous sync.
            status, err := p.podCache.GetNewerThan(podUID, lastSyncTime)
            if err != nil {
                return err
            }
            // 回调新建podWorkers时传入的每个pod同步需要执行的函数
            err = p.syncPodFn(syncPodOptions{
                mirrorPod:      update.MirrorPod,
                pod:            update.Pod,
                podStatus:      status,
                killPodOptions: update.KillPodOptions,
                updateType:     update.UpdateType,
            })
            // 记录上次的更新时间
            lastSyncTime = time.Now()
            return err
        }()
        // notify the call-back function if the operation succeeded or not
        // 如果设置了更新完毕回调的函数,则执行更新完毕的回调函数
        if update.OnCompleteFunc != nil {
            update.OnCompleteFunc(err)
        }
        if err != nil {
            glog.Errorf("Error syncing pod %s (%q), skipping: %v", update.Pod.UID, format.Pod(update.Pod), err)
            // if we failed sync, we throw more specific events for why it happened.
            // as a result, i question the value of this event.
            // TODO: determine if we can remove this in a future release.
            // do not include descriptive text that can vary on why it failed so in a pathological
            // scenario, kubelet does not create enough discrete events that miss default aggregation
            // window.
            p.recorder.Eventf(update.Pod, v1.EventTypeWarning, events.FailedSync, "Error syncing pod")
        }
        // 将当前pod对应的groutine从工作状态退出,去发现是否由等待的更新消息需要处理,如果有等待的更新消息，
        // 则继续进行处理更新消息,如果没有更新消息了则将该groutine设置为空闲状态，随时等待处理更新消息
        p.wrapUp(update.Pod.UID, err)
    }
}

// 将当前pod对应的groutine从工作状态退出,去发现是否由等待的更新消息需要处理,如果有等待的更新消息，
// 则继续进行处理更新消息,如果没有更新消息了则将该groutine设置为空闲状态，随时等待处理更新消息
func (p *podWorkers) wrapUp(uid types.UID, syncErr error) {
    // Requeue the last update if the last sync returned error.
    switch {
    case syncErr == nil:
        // No error; requeue at the regular resync interval.
        p.workQueue.Enqueue(uid, wait.Jitter(p.resyncInterval, workerResyncIntervalJitterFactor))
    default:
        // Error occurred during the sync; back off and then retry.
        p.workQueue.Enqueue(uid, wait.Jitter(p.backOffPeriod, workerBackOffPeriodJitterFactor))
    }
    p.checkForUpdates(uid)
}

// 去发现是否由等待的更新消息需要处理,如果有等待的更新消息，
// 则继续进行处理更新消息,如果没有更新消息了则将该groutine设置为空闲状态，随时等待处理更新消息
func (p *podWorkers) checkForUpdates(uid types.UID) {
    p.podLock.Lock()
    defer p.podLock.Unlock()
    if workUpdate, exists := p.lastUndeliveredWorkUpdate[uid]; exists {
        p.podUpdates[uid] <- workUpdate
        delete(p.lastUndeliveredWorkUpdate, uid)
    } else {
        p.isWorking[uid] = false
    }
}
```
