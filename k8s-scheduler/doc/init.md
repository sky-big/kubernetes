# kubernetes启动初始化流程

## 1. scheduler入口处(k8s.io/kubernetes/plugin/cmd/scheduler.go):

* 先初始化SchedulerServer对象
* 然后通过s.AddFlags(pflag.CommandLine)解释命令行参数
* 再进行命令行参数解析以及日志初始化工作
* 最后启动SchedulerServer对象的Run方法运行scheduler组件

```
func main() {
	// 初始化SchedulerServer对象
	s := options.NewSchedulerServer()
	s.AddFlags(pflag.CommandLine)

	// 初始化命令行参数解析相关
	flag.InitFlags()
	// 初始化日志相关,使用glog有间隔的刷新日志到磁盘
	logs.InitLogs()
	// 在关闭组件的时候将还没有刷新到磁盘的日志写入到磁盘
	defer logs.FlushLogs()

	verflag.PrintAndExitIfRequested()

    // 运行SchedulerServer对象Run方法启动调度组件
	if err := app.Run(s); err != nil {
		glog.Fatalf("scheduler app failed to run: %v", err)
	}
}
```

## 2. 初始化SchedulerServer对象(k8s.io/kubernetes/plugin/cmd/options/options.go):

* 首先使用k8s.io/kubernetes/pkg/api包的Scheme变量(kubernetes项目中所有的变量及该变量对应的版本号都会注册到这个Scheme变量中,同时变量对应获取默认值的函数也会注册进去)中的v1alpha1包中KubeSchedulerConfiguration变量对应的默认函数进行初始化KubeSchedulerConfiguration变量
* 然后将v1alpha1包中的KubeSchedulerConfiguration的变量和componentconfig包中的该对应的对象进行合并,使用正式版本的KubeSchedulerConfiguration对象
     
```
// 初始化SchedulerServer对象
func NewSchedulerServer() *SchedulerServer {
    // 获得scheduler config的默认值
    versioned := &v1alpha1.KubeSchedulerConfiguration{}
    // 调用scheduler config默认的初始化函数对配置对象进行初始化
    api.Scheme.Default(versioned)
    // 将scheduler config对象进行合并,使用正式版本的SchedulerConfig对象
    cfg := componentconfig.KubeSchedulerConfiguration{}
    api.Scheme.Convert(versioned, &cfg, nil)
    cfg.LeaderElection.LeaderElect = true
    s := SchedulerServer{
        KubeSchedulerConfiguration: cfg,
    }
    return &s
}
```

## 3. 然后开始运行SchedulerServer对象的Run方法(k8s.io/kubernetes/plugin/cmd/kube-scheduler/app/server.go):

* 首先创建连接kube-apiserver的客户端
* 然后创建informer工厂对象(里面包含了全部资源对应的share index informer)
* 单独创建pod informer(因为该informer跟工厂里面的不一样,在scheduler组件中pod informer需要直接过滤掉已经停止运行的pod)
* 然后调用k8s.io/kubernetes/plugin/cmd/kubek-scheduler/app/configurator.go中的CreateScheduler方法进行创建真实的scheduler对象
* 继续就是启动debug和健康检查的http服务
* 启动工厂里面的所有informer,每个informer对应一个groutine,pod informer是独立于informer工厂的,所以需要单独启动pod informer,自然pod informer 对应于一个groutine
* 最后是scheduler高可用选取leader对象,然后运行生成的scheduler对象的Run方法

```
// Scheduler组件的运行主函数
func Run(s *options.SchedulerServer) error {
    // 创建连接kube-apiserver的客户端对象
    kubecli, err := createClient(s)
    if err != nil {
        return fmt.Errorf("unable to create kube client: %v", err)
    }

    recorder := createRecorder(kubecli, s)

    // 创建Informer对象工厂
    informerFactory := informers.NewSharedInformerFactory(kubecli, 0)
    // cache only non-terminal pods
    // 单独创建pod informer,此处特殊处理,scheduler组件给自己的pod informer添加了pod过滤标签,
    // 只关心尚未调度的pod
    podInformer := factory.NewPodInformer(kubecli, 0)

    // 创建scheduler对象
    sched, err := CreateScheduler(
        s,
        kubecli,
        informerFactory.Core().V1().Nodes(),
        podInformer,
        informerFactory.Core().V1().PersistentVolumes(),
        informerFactory.Core().V1().PersistentVolumeClaims(),
        informerFactory.Core().V1().ReplicationControllers(),
        informerFactory.Extensions().V1beta1().ReplicaSets(),
        informerFactory.Apps().V1beta1().StatefulSets(),
        informerFactory.Core().V1().Services(),
        recorder,
    )
    if err != nil {
        return fmt.Errorf("error creating scheduler: %v", err)
    }

    // 启动debug和健康检查的http服务
    go startHTTP(s)

    // 停止的chan
    stop := make(chan struct{})
    defer close(stop)
    // 因为podinformer不是从工厂里面启动的,所以需要在此处单独进行启动
    go podInformer.Informer().Run(stop)
    // 将工厂里面的所有informer启动
    informerFactory.Start(stop)
    // Waiting for all cache to sync before scheduling.
    // 等待工厂里面所有的informer同步到缓存完毕
    informerFactory.WaitForCacheSync(stop)
    // 等待podinformer的同步
    controller.WaitForCacheSync("scheduler", stop, podInformer.Informer().HasSynced)

    run := func(_ <-chan struct{}) {
        sched.Run()
        select {}
    }

    if !s.LeaderElection.LeaderElect {
        run(nil)
        panic("unreachable")
    }

    // 获取hostname
    id, err := os.Hostname()
    if err != nil {
        return fmt.Errorf("unable to get hostname: %v", err)
    }

    // 创建资源锁
    rl, err := resourcelock.New(s.LeaderElection.ResourceLock,
        s.LockObjectNamespace,
        s.LockObjectName,
        kubecli,
        resourcelock.ResourceLockConfig{
            Identity:      id,
            EventRecorder: recorder,
        })
    if err != nil {
        glog.Fatalf("error creating lock: %v", err)
    }

    // scheduler高可用选取leader对象的运行
    leaderelection.RunOrDie(leaderelection.LeaderElectionConfig{
        Lock:          rl,
        LeaseDuration: s.LeaderElection.LeaseDuration.Duration,
        RenewDeadline: s.LeaderElection.RenewDeadline.Duration,
        RetryPeriod:   s.LeaderElection.RetryPeriod.Duration,
        Callbacks: leaderelection.LeaderCallbacks{
            OnStartedLeading: run,
            OnStoppedLeading: func() {
                glog.Fatalf("lost master")
            },
        },
    })

    panic("unreachable")
}
```

## 4. 初始化真实的Scheduler对象(k8s.io/kubernetes/plugin/cmd/kube-scheduler/app/configurator.go):

* 先调用scheduler中的工厂模式启动工厂对应的配置对象
* 然后根据工厂配置对象组建本地配置对象,同时在本地对象中加入了scheduler组件中policy对应的信息(包括policy对应的命令行参数或者policy对应的ConfigMap对象的相关信息),使用这些policy用来获取调度器的配置信息
* 最后根据policy信息创建scheduler对象或者使用默认的调度器启动调度对象

```
// 创建scheduler对象
func CreateScheduler(
    s *options.SchedulerServer,
    kubecli *clientset.Clientset,
    nodeInformer coreinformers.NodeInformer,
    podInformer coreinformers.PodInformer,
    pvInformer coreinformers.PersistentVolumeInformer,
    pvcInformer coreinformers.PersistentVolumeClaimInformer,
    replicationControllerInformer coreinformers.ReplicationControllerInformer,
    replicaSetInformer extensionsinformers.ReplicaSetInformer,
    statefulSetInformer appsinformers.StatefulSetInformer,
    serviceInformer coreinformers.ServiceInformer,
    recorder record.EventRecorder,
) (*scheduler.Scheduler, error) {
    // 创建config工厂对象
    configurator := factory.NewConfigFactory(
        s.SchedulerName,
        kubecli,
        nodeInformer,
        podInformer,
        pvInformer,
        pvcInformer,
        replicationControllerInformer,
        replicaSetInformer,
        statefulSetInformer,
        serviceInformer,
        s.HardPodAffinitySymmetricWeight,
    )

    // Rebuild the configurator with a default Create(...) method.
    // 依据工厂config对象创建本地config对象
    configurator = &schedulerConfigurator{
        configurator,
        s.PolicyConfigFile,
        s.AlgorithmProvider,
        s.PolicyConfigMapName,
        s.PolicyConfigMapNamespace,
        s.UseLegacyPolicyConfig,
    }

    // 实际的创建scheduler对象
    return scheduler.NewFromConfigurator(configurator, func(cfg *scheduler.Config) {
        cfg.Recorder = recorder
    })
}
```

## 5. 启动调度器工厂的配置对象(k8s.io/kubernetes/plugin/pkg/schedler/factory/factory.go):

* 首先启动调度器缓存对象
* 然后构建调度器工厂配置对象,同时将除了pod以为的informer进行监听
* 接着是pod informer注册将成功调度的pod添加到缓存的事件,同时将注册将尚未调度的pod添加到podQueue队列中,然后调度器每次都是一直循环从podQueue中获取pod进行调度 
* 最后是对node informer注册事件,将node添加到调度缓存对象中去

```
v1.PodSucceeded: 表示pod中的所有容器自愿全部终止掉,然后这些容器都不在进行重启(所有容器退出都是返回的是0)
v1.PodFailed:    表示pod中的所有容器全部终止掉,同时至少有一个容器退出的时候不是返回的0
```

```
// 创建config工厂对象
func NewConfigFactory(
    schedulerName string,
    client clientset.Interface,
    nodeInformer coreinformers.NodeInformer,
    podInformer coreinformers.PodInformer,
    pvInformer coreinformers.PersistentVolumeInformer,
    pvcInformer coreinformers.PersistentVolumeClaimInformer,
    replicationControllerInformer coreinformers.ReplicationControllerInformer,
    replicaSetInformer extensionsinformers.ReplicaSetInformer,
    statefulSetInformer appsinformers.StatefulSetInformer,
    serviceInformer coreinformers.ServiceInformer,
    hardPodAffinitySymmetricWeight int,
) scheduler.Configurator {
    // 关闭所有对象的信号对象
    stopEverything := make(chan struct{})
    // 初始化scheduler组件的缓存对象
    schedulerCache := schedulercache.New(30*time.Second, stopEverything)

    // 初始化config工厂对象
    c := &ConfigFactory{
        client:                         client,
        podLister:                      schedulerCache,
        podQueue:                       cache.NewFIFO(cache.MetaNamespaceKeyFunc),
        pVLister:                       pvInformer.Lister(),
        pVCLister:                      pvcInformer.Lister(),
        serviceLister:                  serviceInformer.Lister(),
        controllerLister:               replicationControllerInformer.Lister(),
        replicaSetLister:               replicaSetInformer.Lister(),
        statefulSetLister:              statefulSetInformer.Lister(),
        schedulerCache:                 schedulerCache,
        StopEverything:                 stopEverything,
        schedulerName:                  schedulerName,
        hardPodAffinitySymmetricWeight: hardPodAffinitySymmetricWeight,
    }

    // 得到pod的informer是否已经同步的标识
    c.scheduledPodsHasSynced = podInformer.Informer().HasSynced
    // scheduled pod cache
    // 过滤出已经中断运行的pod,然后将已经成功调度的pod添加到缓存中去
    podInformer.Informer().AddEventHandler(
        cache.FilteringResourceEventHandler{
            FilterFunc: func(obj interface{}) bool {
                switch t := obj.(type) {
                case *v1.Pod:
                    return assignedNonTerminatedPod(t)
                default:
                    runtime.HandleError(fmt.Errorf("unable to handle object in %T: %T", c, obj))
                    return false
                }
            },
            Handler: cache.ResourceEventHandlerFuncs{
                AddFunc:    c.addPodToCache,
                UpdateFunc: c.updatePodInCache,
                DeleteFunc: c.deletePodFromCache,
            },
        },
    )
    // unscheduled pod queue
    // 过滤掉已经终止运行状态的`pod然后将这些尚未调度的pod添加到podQueue中进行处理
    podInformer.Informer().AddEventHandler(
        cache.FilteringResourceEventHandler{
            FilterFunc: func(obj interface{}) bool {
                switch t := obj.(type) {
                case *v1.Pod:
                    return unassignedNonTerminatedPod(t)
                default:
                    runtime.HandleError(fmt.Errorf("unable to handle object in %T: %T", c, obj))
                    return false
                }
            },
            Handler: cache.ResourceEventHandlerFuncs{
                AddFunc: func(obj interface{}) {
                    if err := c.podQueue.Add(obj); err != nil {
                        runtime.HandleError(fmt.Errorf("unable to queue %T: %v", obj, err))
                    }
                },
                UpdateFunc: func(oldObj, newObj interface{}) {
                    if err := c.podQueue.Update(newObj); err != nil {
                        runtime.HandleError(fmt.Errorf("unable to update %T: %v", newObj, err))
                    }
                },
                DeleteFunc: func(obj interface{}) {
                    if err := c.podQueue.Delete(obj); err != nil {
                        runtime.HandleError(fmt.Errorf("unable to dequeue %T: %v", obj, err))
                    }
                },
            },
        },
    )
    // ScheduledPodLister is something we provide to plug-in functions that
    // they may need to call.
    // 在上面分开对不同状态的pod进行处理事件处理之后启动pod informer对象进行监听
    c.scheduledPodLister = assignedPodLister{podInformer.Lister()}

    // Only nodes in the "Ready" condition with status == "True" are schedulable
    // 为node informer注册处理事件
    nodeInformer.Informer().AddEventHandlerWithResyncPeriod(
        cache.ResourceEventHandlerFuncs{
            AddFunc:    c.addNodeToCache,
            UpdateFunc: c.updateNodeInCache,
            DeleteFunc: c.deleteNodeFromCache,
        },
        0,
    )
    // 启动node informer对象
    c.nodeLister = nodeInformer.Lister()

    // TODO(harryz) need to fill all the handlers here and below for equivalence cache

    return c
}
```

