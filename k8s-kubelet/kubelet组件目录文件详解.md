# kubernetes kubelet目录功能详解

```
.
├── cmd
│   └── kubelet                             // kubelet command的相关代码
│       ├── app                             // kubelet app的启动
│       │   ├── options         
│       │   │   ├── container_runtime.go    // 容器启动参数相关,包括从命令行传入的参数
│       │   │   └── options.go              // KubeletServer和KubeletFlags对象的实现
│       │   ├── auth.go
│       │   ├── bootstrap.go
│       │   ├── bootstrap_test.go
│       │   ├── plugins.go
│       │   ├── server.go
│       │   ├── server_linux.go
│       │   ├── server_test.go
│       │   └── server_unsupported.go       //
│       └── kubelet.go                      // kubelet包 main方法入口
│
└── pkg
    ├── kubelet    // scheduler后端核心代码
    │   ├── apis
    │   │
    │   ├── cadvisor
    │   │   
    │   ├── certificate
    │   │   
    │   ├── client
    │   │   
    │   ├── cm
    │   │   
    │   ├── config
    │   │   ├── apiserver.go                // 从apiserver这个源获取pod资源         
    │   │   │   
    │   │   ├── common.go
    │   │   │   
    │   │   ├── config.go                   // podConfig对象,管理从不同的源获取pod资源然后统一发送到唯一一个channel上面
    │   │   │                               // 供主groutine进行pod增删查改相关的处理
    │   │   │   
    │   │   ├── file.go                     // 从file文件获取本节点上面的静态pod资源
    │   │   │   
    │   │   ├── http.go                     // 从URL获取本节点上面的静态pod资源
    │   │   │   
    │   │   ├── sources.go                  // pod源管理相关
    │   │   
    │   ├── configmap
    │   │   
    │   ├── container
    │   │   
    │   ├── custommertics
    │   │   
    │   ├── dockershim
    │   │   
    │   ├── envvars
    │   │   
    │   ├── events
    │   │   
    │   ├── eviction
    │   │   
    │   ├── gpu
    │   │   
    │   ├── images
    │   │   
    │   ├── kuberuntime
    │   │   
    │   ├── leaky
    │   │   
    │   ├── lifecycle
    │   │   
    │   ├── metrics
    │   │   
    │   ├── network
    │   │   
    │   ├── pleg
    │   │   
    │   ├── pod
    │   │   ├── mirror_client.go                // mirror镜像pod的创建删除client
    │   │   │   
    │   │   ├── pod_manager.go                  // pod管理器对象,用来管理本节点上面的所有pod资源(即本节点上面的所有pod都会注册到
    │   │   │                                   // 本管理器对象中)
    │   │   
    │   ├── preemption
    │   │   
    │   ├── prober
    │   │   
    │   ├── qos
    │   │   
    │   ├── remote
    │   │   
    │   ├── rkt
    │   │   
    │   ├── rktshim
    │   │   
    │   ├── secret
    │   │   
    │   ├── server
    │   │   
    │   ├── status
    │   │   
    │   ├── sysctl
    │   │   
    │   ├── types
    │   │   
    │   ├── util
    │   │   
    │   ├── volumemanager
    │   │   
    │   │   
    │   ├── active_deadline.go
    │   │   
    │   ├── disk_manager.go
    │   │   
    │   ├── doc.go
    │   │   
    │   ├── kubelet.go
    │   │   
    │   ├── kubelet_cadvisor.go
    │   │   
    │   ├── kubelet_getters.go
    │   │   
    │   ├── kubelet_network.go
    │   │   
    │   ├── kubelet_node_status.go
    │   │   
    │   ├── kubelet_pods.go
    │   │   
    │   ├── kubelet_resources.go
    │   │   
    │   ├── kubelet_volumes.go
    │   │   
    │   ├── networks.go
    │   │   
    │   ├── oom_watcher.go
    │   │   
    │   ├── pod_container_deletor.go
    │   │   
    │   ├── pod_workers.go
    │   │   
    │   ├── reason_cache.go
    │   │   
    │   ├── runonce.go
    │   │   
    │   ├── runtime.go
    │   │   
    │   ├── util.go
    │   │   
        └── volume_host.go                  
```