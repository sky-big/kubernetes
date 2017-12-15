# kubernetes 调度器目录功能详解

```
.
├── cmd
│   └── kube-scheduler                      // kube-scheduler command的相关代码
│       ├── app                             // kube-scheduler app的启动
│       │   ├── options         
│       │   │   └── options.go              // 封装SchedulerServer对象和AddFlags方法
│       │   ├── configurator.go
│       │   ├── configurator_test.go
│       │   └── server.go                   // 定义SchedulerServer的config封装和Run方法
│       └── scheduler.go                    // kube-scheduler main方法入口
│
└── pkg
    ├── scheduler    // scheduler后端核心代码
    │   ├── algorithm
    │   │   ├── predicates                  // 定义kubernetes自带的Predicates Policies的Function实现
    │   │   │   ├── error.go
    │   │   │   ├── metadata.go
    │   │   │   ├── predicates.go           // 自带Predicates Policies的主要实现
    │   │   │   ├── predicates_test.go
    │   │   │   ├── utils.go
    │   │   │   └── utils_test.go
    │   │   │
    │   │   ├── priorities      // 定义kubernetes自带的Priorities Policies的Function实现
    │   │   │   ├── balanced_resource_allocation.go                 // BalancedResourceAllocation
    │   │   │   ├── balanced_resource_allocation_test.go
    │   │   │   ├── image_locality.go                               // ImageLocalityPriority
    │   │   │   ├── image_locality_test.go
    │   │   │   ├── interpod_affinity.go                            // InterPodAffinityPriority
    │   │   │   ├── interpod_affinity_test.go
    │   │   │   ├── least_requested.go                              // LeastRequestedPriority
    │   │   │   ├── least_requested_test.go 
    │   │   │   ├── metadata.go                                     // priorityMetadata定义
    │   │   │   ├── metadata_test.go
    │   │   │   ├── most_requested.go                               // MostRequestedPriority
    │   │   │   ├── most_requested_test.go
    │   │   │   ├── node_affinity.go                                // NodeAffinityPriority
    │   │   │   ├── node_affinity_test.go
    │   │   │   ├── node_label.go                                   // 当policy.Argument.LabelPreference != nil时，会注册该Policy
    │   │   │   ├── node_label_test.go
    │   │   │   ├── node_prefer_avoid_pods.go                       // NodePreferAvoidPodsPriority 
    │   │   │   ├── node_prefer_avoid_pods_test.go
    │   │   │   ├── selector_spreading.go                           // SelectorSpreadPriority
    │   │   │   ├── selector_spreading_test.go
    │   │   │   ├── taint_toleration.go                             // TaintTolerationPriority
    │   │   │   ├── taint_toleration_test.go
    │   │   │   ├── test_util.go
    │   │   │   └── util         // 工具类
    │   │   │       ├── non_zero.go
    │   │   │       ├── topologies.go
    │   │   │       ├── topologies_test.go
    │   │   │       └── util.go
    │   │   │
    │   │   ├── doc.go
    │   │   ├── scheduler_interface.go              // 定义SchedulerExtender和ScheduleAlgorithm Interface
    │   │   ├── scheduler_interface_test.go
    │   │   └── types.go                            // 定义了Predicates和Priorities Algorithm要实现的方法类型(FitPredicate, PriorityMapFunction)
    │   │
    │   ├── algorithmprovider          // algorithm-provider参数配置的项
    │   │   ├── defaults    
    │   │   │   ├── compatibility_test.go
    │   │   │   └── defaults.go                     // "DefaultProvider"的实现
    │   │   │   └── defaults_test.go
    │   │   ├── plugins.go                          // 空，预留自定义
    │   │   └── plugins_test.go
    │   │
    │   ├── api            // 定义Scheduelr API接口和对象，用于SchedulerExtender处理来自HTTPExtender的请求。
    │   │   ├── latest
    │   │   │   └── latest.go
    │   │   ├── register.go
    │   │   ├── types.go                            // 定义Policy, PredicatePolicy,PriorityPolicy等
    │   │   ├── v1
    │   │   │   ├── register.go
    │   │   │   └── types.go
    │   │   └── validation
    │   │       ├── validation.go                   // 验证Policy的定义是否合法
    │   │       └── validation_test.go
    │   │
    │   ├── core
    │   │   ├── equivalence_cache.go
    │   │   ├── equivalence_cache_test.go
    │   │   ├── extender.go                         // 定义HTTPExtender的新建以及对应的Filter和Prioritize方法来干预预选和优选
    │   │   ├── extender_test.go
    │   │   ├── generic_scheduler.go
    │   │   └── generic_scheduler_test.go
    │   │
    │   ├── factory                     // 根据配置的Policies注册和匹配到对应的预选(FitPredicateFactory)和优选(PriorityFunctionFactory2)函数
    │   │   ├── factory.go                          // 核心是定义ConfigFactory来工具配置完成scheduler的封装函数，最关键的CreateFromConfig和CreateFromKeys
    │   │   ├── factory_test.go
    │   │   ├── plugins.go                          // 核心是定义注册自定义预选和优选Policy的方法
    │   │   └── plugins_test.go
    │   │
    │   ├── metrics                     // 支持注册metrics到Prometheus
    │   │   └── metrics.go
    │   │
    │   ├── schedulercache       
    │   │   ├── cache.go                                // 定义schedulerCache对Pod，Node，以及Bind的CURD，以及超时维护等工作，缓存了集群中的所有节点，包括每个节点下
    │   │   │                                           // 包含的pod等相关信息，同时该缓存中还要管理assumed状态的pod的整个生命周期要调用的接口
    │   │   ├── cache_test.go
    │   │   ├── interface.go                            // schedulerCache要实现的Interface
    │   │   ├── node_info.go                            // 定义NodeInfo及其相关Opertation，每个节点缓存数据结构的定义，用来提供给cache来管理每个node的信息
    │   │   ├── reconcile_affinity.go
    │   │   ├── reconcile_affinity_test.go
    │   │   └── util.go
    │   │
    │   ├── testing
    │   │   ├── fake_cache.go
    │   │   ├── fake_lister.go
    │   │   └── pods_to_cache.go
    │   │
    │   ├── util
    │   │   ├── backoff_utils.go
    │   │   └── backoff_utils_test.go
    │   │
    │   ├── scheduler.go                // 定义Scheduler及Run()，核心的scheduleOne()方法也在此，scheduleOne()一个完成的调度流程，包括或许待调度Pod、调度、Bind等
    │   ├── scheduler_test.go

```
