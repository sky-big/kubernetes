# Kubernetes Kubelet 初始化流程

## 1. 启动入口(k8s.io/kubernetes/cmd/kubelet/kubelet.go):

* 先是初始化kubelet对应options包里面的KubeletServer对象
* 然后是进行命令行参数和日志相关的初始化
* 根据参数设置是否启动docker shim模式
* 最后根据KubeletServer对象使用app包的Run方法启动kubelet

```
func main() {
	// 初始化kubelet对应options包里面的KubeletServer对象
	s := options.NewKubeletServer()
	// 添加命令行参数
	s.AddFlags(pflag.CommandLine)

	// 初始化命令行参数
	flag.InitFlags()
	// 初始化日志设置
	logs.InitLogs()
	// 最后需要将还没有刷新到磁盘的日志刷新到磁盘上
	defer logs.FlushLogs()

	verflag.PrintAndExitIfRequested()

	// 启动docker shim模式
	if s.ExperimentalDockershim {
		if err := app.RunDockershim(&s.KubeletConfiguration, &s.ContainerRuntimeOptions); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	// 启动app中的Run方法来启动kubelet
	if err := app.Run(s, nil); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
```

## 2. 上一步骤中对于KubeletServer对象初始化(k8s.io/kubernetes/cmd/kubelet/app/options):

* 初始化v1alpha1版本下的kubelet配置对象
* 使用注册的默认初始化函数初始化kubelet配置对象
* 最后初始化kubelet专门的命令行参数配置对象,然后根据命令行参数对象和kubelet配置对象创建KubeletServer对象

```
func NewKubeletServer() *KubeletServer {
	// 初始化v1alpha1版本下的kubelet配置对象
	versioned := &v1alpha1.KubeletConfiguration{}
	// 使用注册的默认初始化函数初始化kubelet配置对象
	api.Scheme.Default(versioned)
	config := componentconfig.KubeletConfiguration{}
	// 合并kubelet配置对象
	api.Scheme.Convert(versioned, &config, nil)
	// 初始化kubelet专门的命令行参数配置对象,然后根据命令行参数对象和kubelet配置对象创建KubeletServer对象
	return &KubeletServer{
		KubeletFlags: KubeletFlags{
			KubeConfig:              flag.NewStringFlag("/var/lib/kubelet/kubeconfig"),
			RequireKubeConfig:       false,
			ContainerRuntimeOptions: *NewContainerRuntimeOptions(),
		},
		KubeletConfiguration: config,
	}
}
```

## 3. 第1步骤会调用本步骤启动kubelet之前的准备工作(k8s.io/kubernetes/cmd/kubelet/app/server.go):

* 首先尝试从apiserver去获取kubelet的配置信息
* 验证配置信息的正确性
* 创建kubeDeps对象(该对象里面会有cloudprovider,连接apiserver的各种客户端对象)
* 创建cadvisor对象
* 创建container manager对象(该对象会存储在kubeDeps对象中)
* 最后启动kubelet对象

```
// Run runs the specified KubeletServer with the given KubeletDeps.  This should never exit.
// The kubeDeps argument may be nil - if so, it is initialized from the settings on KubeletServer.
// Otherwise, the caller is assumed to have set up the KubeletDeps object and a default one will
// not be generated.
func Run(s *options.KubeletServer, kubeDeps *kubelet.KubeletDeps) error {
	if err := run(s, kubeDeps); err != nil {
		return fmt.Errorf("failed to run Kubelet: %v", err)
	}
	return nil
}
```

```
// 启动kubelet
func run(s *options.KubeletServer, kubeDeps *kubelet.KubeletDeps) (err error) {
	// TODO: this should be replaced by a --standalone flag
	standaloneMode := (len(s.APIServerList) == 0 && !s.RequireKubeConfig)

	if s.ExitOnLockContention && s.LockFilePath == "" {
		return errors.New("cannot exit on lock file contention: no lock file specified")
	}

	done := make(chan struct{})
	if s.LockFilePath != "" {
		glog.Infof("acquiring file lock on %q", s.LockFilePath)
		if err := flock.Acquire(s.LockFilePath); err != nil {
			return fmt.Errorf("unable to acquire file lock on %q: %v", s.LockFilePath, err)
		}
		if s.ExitOnLockContention {
			glog.Infof("watching for inotify events for: %v", s.LockFilePath)
			if err := watchForLockfileContention(s.LockFilePath, done); err != nil {
				return err
			}
		}
	}

	// Set feature gates based on the value in KubeletConfiguration
	err = utilfeature.DefaultFeatureGate.Set(s.KubeletConfiguration.FeatureGates)
	if err != nil {
		return err
	}

	// Register current configuration with /configz endpoint
	cfgz, cfgzErr := initConfigz(&s.KubeletConfiguration)
	// 首先尝试从apiserver去获取kubelet的配置信息
	if utilfeature.DefaultFeatureGate.Enabled(features.DynamicKubeletConfig) {
		// Look for config on the API server. If it exists, replace s.KubeletConfiguration
		// with it and continue. initKubeletConfigSync also starts the background thread that checks for new config.

		// Don't do dynamic Kubelet configuration in runonce mode
		if s.RunOnce == false {
			// 尝试去apiserver去获取kubelet的配置文件
			remoteKC, err := initKubeletConfigSync(s)
			if err == nil {
				// Update s (KubeletServer) with new config from API server
				// 如果获取成功则赋值给KubeletServer对象中的配置对象
				s.KubeletConfiguration = *remoteKC
				// Ensure that /configz is up to date with the new config
				if cfgzErr != nil {
					glog.Errorf("was unable to register configz before due to %s, will not be able to set now", cfgzErr)
				} else {
					setConfigz(cfgz, &s.KubeletConfiguration)
				}
				// Update feature gates from the new config
				err = utilfeature.DefaultFeatureGate.Set(s.KubeletConfiguration.FeatureGates)
				if err != nil {
					return err
				}
			} else {
				glog.Errorf("failed to init dynamic Kubelet configuration sync: %v", err)
			}
		}
	}

	// Validate configuration.
	// 验证配置信息的正确性
	if err := validateConfig(s); err != nil {
		return err
	}

	// 创建kubeDeps对象
	if kubeDeps == nil {
		var kubeClient clientset.Interface
		var eventClient v1core.EventsGetter
		var externalKubeClient clientgoclientset.Interface
		var cloud cloudprovider.Interface

		// 创建cloudprovider对象
		if !cloudprovider.IsExternal(s.CloudProvider) && s.CloudProvider != componentconfigv1alpha1.AutoDetectCloudProvider {
			cloud, err = cloudprovider.InitCloudProvider(s.CloudProvider, s.CloudConfigFile)
			if err != nil {
				return err
			}
			if cloud == nil {
				glog.V(2).Infof("No cloud provider specified: %q from the config file: %q\n", s.CloudProvider, s.CloudConfigFile)
			} else {
				glog.V(2).Infof("Successfully initialized cloud provider: %q from the config file: %q\n", s.CloudProvider, s.CloudConfigFile)
			}
		}

		nodeName, err := getNodeName(cloud, nodeutil.GetHostname(s.HostnameOverride))
		if err != nil {
			return err
		}

		if s.BootstrapKubeconfig != "" {
			if err := bootstrapClientCert(s.KubeConfig.Value(), s.BootstrapKubeconfig, s.CertDirectory, nodeName); err != nil {
				return err
			}
		}

		// 创建连接apiserver的客户端配置对象
		clientConfig, err := CreateAPIServerClientConfig(s)

		var clientCertificateManager certificate.Manager
		// 创建连接apiserver的客户端,包括额外以及事件客户端
		if err == nil {
			if utilfeature.DefaultFeatureGate.Enabled(features.RotateKubeletClientCertificate) {
				nodeName, err := getNodeName(cloud, nodeutil.GetHostname(s.HostnameOverride))
				if err != nil {
					return err
				}
				clientCertificateManager, err = initializeClientCertificateManager(s.CertDirectory, nodeName, clientConfig.CertData, clientConfig.KeyData, clientConfig.CertFile, clientConfig.KeyFile)
				if err != nil {
					return err
				}
				if err := updateTransport(clientConfig, clientCertificateManager); err != nil {
					return err
				}
			}

			kubeClient, err = clientset.NewForConfig(clientConfig)
			if err != nil {
				glog.Warningf("New kubeClient from clientConfig error: %v", err)
			} else if kubeClient.Certificates() != nil && clientCertificateManager != nil {
				glog.V(2).Info("Starting client certificate rotation.")
				clientCertificateManager.SetCertificateSigningRequestClient(kubeClient.Certificates().CertificateSigningRequests())
				clientCertificateManager.Start()
			}
			externalKubeClient, err = clientgoclientset.NewForConfig(clientConfig)
			if err != nil {
				glog.Warningf("New kubeClient from clientConfig error: %v", err)
			}
			// make a separate client for events
			eventClientConfig := *clientConfig
			eventClientConfig.QPS = float32(s.EventRecordQPS)
			eventClientConfig.Burst = int(s.EventBurst)
			eventClient, err = clientgoclientset.NewForConfig(&eventClientConfig)
			if err != nil {
				glog.Warningf("Failed to create API Server client: %v", err)
			}
		} else {
			if s.RequireKubeConfig {
				return fmt.Errorf("invalid kubeconfig: %v", err)
			} else if s.KubeConfig.Provided() && !standaloneMode {
				glog.Warningf("Invalid kubeconfig: %v", err)
			}
			if standaloneMode {
				glog.Warningf("No API client: %v", err)
			}
		}

		kubeDeps, err = UnsecuredKubeletDeps(s)
		if err != nil {
			return err
		}

		kubeDeps.Cloud = cloud
		kubeDeps.KubeClient = kubeClient
		kubeDeps.ExternalKubeClient = externalKubeClient
		kubeDeps.EventClient = eventClient
	}

	// 获取节点名字
	nodeName, err := getNodeName(kubeDeps.Cloud, nodeutil.GetHostname(s.HostnameOverride))
	if err != nil {
		return err
	}

	// 创建验证对象
	if kubeDeps.Auth == nil {
		auth, err := BuildAuth(nodeName, kubeDeps.ExternalKubeClient, s.KubeletConfiguration)
		if err != nil {
			return err
		}
		kubeDeps.Auth = auth
	}

	// 创建cadvisor对象
	if kubeDeps.CAdvisorInterface == nil {
		kubeDeps.CAdvisorInterface, err = cadvisor.New(uint(s.CAdvisorPort), s.ContainerRuntime, s.RootDirectory)
		if err != nil {
			return err
		}
	}

	// Setup event recorder if required.
	makeEventRecorder(&s.KubeletConfiguration, kubeDeps, nodeName)

	// 创建container manager对象
	if kubeDeps.ContainerManager == nil {
		if s.CgroupsPerQOS && s.CgroupRoot == "" {
			glog.Infof("--cgroups-per-qos enabled, but --cgroup-root was not specified.  defaulting to /")
			s.CgroupRoot = "/"
		}
		kubeReserved, err := parseResourceList(s.KubeReserved)
		if err != nil {
			return err
		}
		systemReserved, err := parseResourceList(s.SystemReserved)
		if err != nil {
			return err
		}
		var hardEvictionThresholds []evictionapi.Threshold
		// If the user requested to ignore eviction thresholds, then do not set valid values for hardEvictionThresholds here.
		if !s.ExperimentalNodeAllocatableIgnoreEvictionThreshold {
			hardEvictionThresholds, err = eviction.ParseThresholdConfig([]string{}, s.EvictionHard, "", "", "")
			if err != nil {
				return err
			}
		}
		experimentalQOSReserved, err := cm.ParseQOSReserved(s.ExperimentalQOSReserved)
		if err != nil {
			return err
		}
		kubeDeps.ContainerManager, err = cm.NewContainerManager(
			kubeDeps.Mounter,
			kubeDeps.CAdvisorInterface,
			cm.NodeConfig{
				RuntimeCgroupsName:    s.RuntimeCgroups,
				SystemCgroupsName:     s.SystemCgroups,
				KubeletCgroupsName:    s.KubeletCgroups,
				ContainerRuntime:      s.ContainerRuntime,
				CgroupsPerQOS:         s.CgroupsPerQOS,
				CgroupRoot:            s.CgroupRoot,
				CgroupDriver:          s.CgroupDriver,
				ProtectKernelDefaults: s.ProtectKernelDefaults,
				NodeAllocatableConfig: cm.NodeAllocatableConfig{
					KubeReservedCgroupName:   s.KubeReservedCgroup,
					SystemReservedCgroupName: s.SystemReservedCgroup,
					EnforceNodeAllocatable:   sets.NewString(s.EnforceNodeAllocatable...),
					KubeReserved:             kubeReserved,
					SystemReserved:           systemReserved,
					HardEvictionThresholds:   hardEvictionThresholds,
				},
				ExperimentalQOSReserved: *experimentalQOSReserved,
			},
			s.ExperimentalFailSwapOn,
			kubeDeps.Recorder)

		if err != nil {
			return err
		}
	}

	if err := checkPermissions(); err != nil {
		glog.Error(err)
	}

	utilruntime.ReallyCrash = s.ReallyCrashForTesting

	rand.Seed(time.Now().UTC().UnixNano())

	// TODO(vmarmol): Do this through container config.
	oomAdjuster := kubeDeps.OOMAdjuster
	if err := oomAdjuster.ApplyOOMScoreAdj(0, int(s.OOMScoreAdj)); err != nil {
		glog.Warning(err)
	}

	// 启动kubelet对象
	if err := RunKubelet(&s.KubeletFlags, &s.KubeletConfiguration, kubeDeps, s.RunOnce, standaloneMode); err != nil {
		return err
	}

	if s.HealthzPort > 0 {
		healthz.DefaultHealthz()
		go wait.Until(func() {
			err := http.ListenAndServe(net.JoinHostPort(s.HealthzBindAddress, strconv.Itoa(int(s.HealthzPort))), nil)
			if err != nil {
				glog.Errorf("Starting health server failed: %v", err)
			}
		}, 5*time.Second, wait.NeverStop)
	}

	if s.RunOnce {
		return nil
	}

	<-done
	return nil
}
```

## 4. 上一步骤会调用本步骤启动kubelet(k8s.io/kubernetes/cmd/kubelet/app/server.go):

```
// 启动kubelet
func RunKubelet(kubeFlags *options.KubeletFlags, kubeCfg *componentconfig.KubeletConfiguration, kubeDeps *kubelet.KubeletDeps, runOnce bool, standaloneMode bool) error {
	hostname := nodeutil.GetHostname(kubeFlags.HostnameOverride)
	// Query the cloud provider for our node name, default to hostname if kcfg.Cloud == nil
	nodeName, err := getNodeName(kubeDeps.Cloud, hostname)
	if err != nil {
		return err
	}
	// Setup event recorder if required.
	makeEventRecorder(kubeCfg, kubeDeps, nodeName)

	// TODO(mtaufen): I moved the validation of these fields here, from UnsecuredKubeletConfig,
	//                so that I could remove the associated fields from KubeletConfig. I would
	//                prefer this to be done as part of an independent validation step on the
	//                KubeletConfiguration. But as far as I can tell, we don't have an explicit
	//                place for validation of the KubeletConfiguration yet.
	hostNetworkSources, err := kubetypes.GetValidatedSources(kubeCfg.HostNetworkSources)
	if err != nil {
		return err
	}

	hostPIDSources, err := kubetypes.GetValidatedSources(kubeCfg.HostPIDSources)
	if err != nil {
		return err
	}

	hostIPCSources, err := kubetypes.GetValidatedSources(kubeCfg.HostIPCSources)
	if err != nil {
		return err
	}

	privilegedSources := capabilities.PrivilegedSources{
		HostNetworkSources: hostNetworkSources,
		HostPIDSources:     hostPIDSources,
		HostIPCSources:     hostIPCSources,
	}
	capabilities.Setup(kubeCfg.AllowPrivileged, privilegedSources, 0)

	credentialprovider.SetPreferredDockercfgPath(kubeCfg.RootDirectory)
	glog.V(2).Infof("Using root directory: %v", kubeCfg.RootDirectory)

	builder := kubeDeps.Builder
	if builder == nil {
		builder = CreateAndInitKubelet
	}
	if kubeDeps.OSInterface == nil {
		kubeDeps.OSInterface = kubecontainer.RealOS{}
	}
	// 创建和初始化kubelet对象
	k, err := builder(kubeCfg, kubeDeps, &kubeFlags.ContainerRuntimeOptions, standaloneMode, kubeFlags.HostnameOverride, kubeFlags.NodeIP, kubeFlags.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to create kubelet: %v", err)
	}

	// NewMainKubelet should have set up a pod source config if one didn't exist
	// when the builder was run. This is just a precaution.
	if kubeDeps.PodConfig == nil {
		return fmt.Errorf("failed to create kubelet, pod source config was nil")
	}
	podCfg := kubeDeps.PodConfig

	rlimit.RlimitNumFiles(uint64(kubeCfg.MaxOpenFiles))

	// process pods and exit.
	if runOnce {
		if _, err := k.RunOnce(podCfg.Updates()); err != nil {
			return fmt.Errorf("runonce failed: %v", err)
		}
		glog.Infof("Started kubelet %s as runonce", version.Get().String())
	} else {
		// 真实的启动kubelet
		startKubelet(k, podCfg, kubeCfg, kubeDeps)
		glog.Infof("Started kubelet %s", version.Get().String())
	}
	return nil
}
```

## 5. 上一步骤中调用本步骤进行创建和初始化kubelet对象(k8s.io/kubernetes/cmd/kubelet/app/server.go):

```
// 创建kubelet对象以及初始化kubelet对象
func CreateAndInitKubelet(kubeCfg *componentconfig.KubeletConfiguration, kubeDeps *kubelet.KubeletDeps, crOptions *options.ContainerRuntimeOptions, standaloneMode bool, hostnameOverride, nodeIP, providerID string) (k kubelet.KubeletBootstrap, err error) {
	// TODO: block until all sources have delivered at least one update to the channel, or break the sync loop
	// up into "per source" synchronizations

	k, err = kubelet.NewMainKubelet(kubeCfg, kubeDeps, crOptions, standaloneMode, hostnameOverride, nodeIP, providerID)
	if err != nil {
		return nil, err
	}

	k.BirthCry()

	k.StartGarbageCollection()

	return k, nil
}
```

## 6. 上一步骤中会调用本步骤创建kubelet对象(k8s.io/kubernetes/pkg/kubelet/kubelet.go):

```
// NewMainKubelet instantiates a new Kubelet object along with all the required internal modules.
// No initialization of Kubelet and its modules should happen here.
func NewMainKubelet(kubeCfg *componentconfig.KubeletConfiguration, kubeDeps *KubeletDeps, crOptions *options.ContainerRuntimeOptions, standaloneMode bool, hostnameOverride, nodeIP, providerID string) (*Kubelet, error) {
	if kubeCfg.RootDirectory == "" {
		return nil, fmt.Errorf("invalid root directory %q", kubeCfg.RootDirectory)
	}
	if kubeCfg.SyncFrequency.Duration <= 0 {
		return nil, fmt.Errorf("invalid sync frequency %d", kubeCfg.SyncFrequency.Duration)
	}

	if kubeCfg.MakeIPTablesUtilChains {
		if kubeCfg.IPTablesMasqueradeBit > 31 || kubeCfg.IPTablesMasqueradeBit < 0 {
			return nil, fmt.Errorf("iptables-masquerade-bit is not valid. Must be within [0, 31]")
		}
		if kubeCfg.IPTablesDropBit > 31 || kubeCfg.IPTablesDropBit < 0 {
			return nil, fmt.Errorf("iptables-drop-bit is not valid. Must be within [0, 31]")
		}
		if kubeCfg.IPTablesDropBit == kubeCfg.IPTablesMasqueradeBit {
			return nil, fmt.Errorf("iptables-masquerade-bit and iptables-drop-bit must be different")
		}
	}

	hostname := nodeutil.GetHostname(hostnameOverride)
	// Query the cloud provider for our node name, default to hostname
	nodeName := types.NodeName(hostname)
	cloudIPs := []net.IP{}
	cloudNames := []string{}
	if kubeDeps.Cloud != nil {
		var err error
		instances, ok := kubeDeps.Cloud.Instances()
		if !ok {
			return nil, fmt.Errorf("failed to get instances from cloud provider")
		}

		nodeName, err = instances.CurrentNodeName(hostname)
		if err != nil {
			return nil, fmt.Errorf("error fetching current instance name from cloud provider: %v", err)
		}

		glog.V(2).Infof("cloud provider determined current node name to be %s", nodeName)

		if utilfeature.DefaultFeatureGate.Enabled(features.RotateKubeletServerCertificate) {
			nodeAddresses, err := instances.NodeAddresses(nodeName)
			if err != nil {
				return nil, fmt.Errorf("failed to get the addresses of the current instance from the cloud provider: %v", err)
			}
			for _, nodeAddress := range nodeAddresses {
				switch nodeAddress.Type {
				case v1.NodeExternalIP, v1.NodeInternalIP:
					ip := net.ParseIP(nodeAddress.Address)
					if ip != nil && !ip.IsLoopback() {
						cloudIPs = append(cloudIPs, ip)
					}
				case v1.NodeExternalDNS, v1.NodeInternalDNS, v1.NodeHostName:
					cloudNames = append(cloudNames, nodeAddress.Address)
				}
			}
		}

	}

    // 初始化podConfig对象,该对象从不同的源获取pod对象
	if kubeDeps.PodConfig == nil {
		var err error
		kubeDeps.PodConfig, err = makePodSourceConfig(kubeCfg, kubeDeps, nodeName)
		if err != nil {
			return nil, err
		}
	}

	containerGCPolicy := kubecontainer.ContainerGCPolicy{
		MinAge:             kubeCfg.MinimumGCAge.Duration,
		MaxPerPodContainer: int(kubeCfg.MaxPerPodContainerCount),
		MaxContainers:      int(kubeCfg.MaxContainerCount),
	}

	daemonEndpoints := &v1.NodeDaemonEndpoints{
		KubeletEndpoint: v1.DaemonEndpoint{Port: kubeCfg.Port},
	}

	imageGCPolicy := images.ImageGCPolicy{
		MinAge:               kubeCfg.ImageMinimumGCAge.Duration,
		HighThresholdPercent: int(kubeCfg.ImageGCHighThresholdPercent),
		LowThresholdPercent:  int(kubeCfg.ImageGCLowThresholdPercent),
	}

	diskSpacePolicy := DiskSpacePolicy{
		DockerFreeDiskMB: int(kubeCfg.LowDiskSpaceThresholdMB),
		RootFreeDiskMB:   int(kubeCfg.LowDiskSpaceThresholdMB),
	}

	enforceNodeAllocatable := kubeCfg.EnforceNodeAllocatable
	if kubeCfg.ExperimentalNodeAllocatableIgnoreEvictionThreshold {
		// Do not provide kubeCfg.EnforceNodeAllocatable to eviction threshold parsing if we are not enforcing Evictions
		enforceNodeAllocatable = []string{}
	}
	thresholds, err := eviction.ParseThresholdConfig(enforceNodeAllocatable, kubeCfg.EvictionHard, kubeCfg.EvictionSoft, kubeCfg.EvictionSoftGracePeriod, kubeCfg.EvictionMinimumReclaim)
	if err != nil {
		return nil, err
	}
	evictionConfig := eviction.Config{
		PressureTransitionPeriod: kubeCfg.EvictionPressureTransitionPeriod.Duration,
		MaxPodGracePeriodSeconds: int64(kubeCfg.EvictionMaxPodGracePeriod),
		Thresholds:               thresholds,
		KernelMemcgNotification:  kubeCfg.ExperimentalKernelMemcgNotification,
	}

	serviceIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if kubeDeps.KubeClient != nil {
		serviceLW := cache.NewListWatchFromClient(kubeDeps.KubeClient.Core().RESTClient(), "services", metav1.NamespaceAll, fields.Everything())
		cache.NewReflector(serviceLW, &v1.Service{}, serviceIndexer, 0).Run()
	}
	serviceLister := corelisters.NewServiceLister(serviceIndexer)

	nodeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if kubeDeps.KubeClient != nil {
		fieldSelector := fields.Set{api.ObjectNameField: string(nodeName)}.AsSelector()
		nodeLW := cache.NewListWatchFromClient(kubeDeps.KubeClient.Core().RESTClient(), "nodes", metav1.NamespaceAll, fieldSelector)
		cache.NewReflector(nodeLW, &v1.Node{}, nodeIndexer, 0).Run()
	}
	nodeInfo := &predicates.CachedNodeInfo{NodeLister: corelisters.NewNodeLister(nodeIndexer)}

	// TODO: get the real node object of ourself,
	// and use the real node name and UID.
	// TODO: what is namespace for node?
	nodeRef := &clientv1.ObjectReference{
		Kind:      "Node",
		Name:      string(nodeName),
		UID:       types.UID(nodeName),
		Namespace: "",
	}

	diskSpaceManager, err := newDiskSpaceManager(kubeDeps.CAdvisorInterface, diskSpacePolicy)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize disk manager: %v", err)
	}
	containerRefManager := kubecontainer.NewRefManager()

	oomWatcher := NewOOMWatcher(kubeDeps.CAdvisorInterface, kubeDeps.Recorder)

	clusterDNS := make([]net.IP, 0, len(kubeCfg.ClusterDNS))
	for _, ipEntry := range kubeCfg.ClusterDNS {
		ip := net.ParseIP(ipEntry)
		if ip == nil {
			glog.Warningf("Invalid clusterDNS ip '%q'", ipEntry)
		} else {
			clusterDNS = append(clusterDNS, ip)
		}
	}
	httpClient := &http.Client{}

	klet := &Kubelet{
		hostname:                       hostname,
		nodeName:                       nodeName,
		kubeClient:                     kubeDeps.KubeClient,
		rootDirectory:                  kubeCfg.RootDirectory,
		resyncInterval:                 kubeCfg.SyncFrequency.Duration,
		sourcesReady:                   config.NewSourcesReady(kubeDeps.PodConfig.SeenAllSources),
		registerNode:                   kubeCfg.RegisterNode,
		registerSchedulable:            kubeCfg.RegisterSchedulable,
		standaloneMode:                 standaloneMode,
		clusterDomain:                  kubeCfg.ClusterDomain,
		clusterDNS:                     clusterDNS,
		serviceLister:                  serviceLister,
		nodeInfo:                       nodeInfo,
		masterServiceNamespace:         kubeCfg.MasterServiceNamespace,
		streamingConnectionIdleTimeout: kubeCfg.StreamingConnectionIdleTimeout.Duration,
		recorder:                       kubeDeps.Recorder,
		cadvisor:                       kubeDeps.CAdvisorInterface,
		diskSpaceManager:               diskSpaceManager,
		cloud:                          kubeDeps.Cloud,
		autoDetectCloudProvider:   (componentconfigv1alpha1.AutoDetectCloudProvider == kubeCfg.CloudProvider),
		externalCloudProvider:     cloudprovider.IsExternal(kubeCfg.CloudProvider),
		providerID:                providerID,
		nodeRef:                   nodeRef,
		nodeLabels:                kubeCfg.NodeLabels,
		nodeStatusUpdateFrequency: kubeCfg.NodeStatusUpdateFrequency.Duration,
		os:               kubeDeps.OSInterface,
		oomWatcher:       oomWatcher,
		cgroupsPerQOS:    kubeCfg.CgroupsPerQOS,
		cgroupRoot:       kubeCfg.CgroupRoot,
		mounter:          kubeDeps.Mounter,
		writer:           kubeDeps.Writer,
		maxPods:          int(kubeCfg.MaxPods),
		podsPerCore:      int(kubeCfg.PodsPerCore),
		syncLoopMonitor:  atomic.Value{},
		resolverConfig:   kubeCfg.ResolverConfig,
		daemonEndpoints:  daemonEndpoints,
		containerManager: kubeDeps.ContainerManager,
		nodeIP:           net.ParseIP(nodeIP),
		clock:            clock.RealClock{},
		outOfDiskTransitionFrequency:            kubeCfg.OutOfDiskTransitionFrequency.Duration,
		enableControllerAttachDetach:            kubeCfg.EnableControllerAttachDetach,
		iptClient:                               utilipt.New(utilexec.New(), utildbus.New(), utilipt.ProtocolIpv4),
		makeIPTablesUtilChains:                  kubeCfg.MakeIPTablesUtilChains,
		iptablesMasqueradeBit:                   int(kubeCfg.IPTablesMasqueradeBit),
		iptablesDropBit:                         int(kubeCfg.IPTablesDropBit),
		experimentalHostUserNamespaceDefaulting: utilfeature.DefaultFeatureGate.Enabled(features.ExperimentalHostUserNamespaceDefaultingGate),
	}

	secretManager := secret.NewCachingSecretManager(
		kubeDeps.KubeClient, secret.GetObjectTTLFromNodeFunc(klet.GetNode))
	klet.secretManager = secretManager

	configMapManager := configmap.NewCachingConfigMapManager(
		kubeDeps.KubeClient, configmap.GetObjectTTLFromNodeFunc(klet.GetNode))
	klet.configMapManager = configMapManager

	if klet.experimentalHostUserNamespaceDefaulting {
		glog.Infof("Experimental host user namespace defaulting is enabled.")
	}

	hairpinMode, err := effectiveHairpinMode(componentconfig.HairpinMode(kubeCfg.HairpinMode), kubeCfg.ContainerRuntime, crOptions.NetworkPluginName)
	if err != nil {
		// This is a non-recoverable error. Returning it up the callstack will just
		// lead to retries of the same failure, so just fail hard.
		glog.Fatalf("Invalid hairpin mode: %v", err)
	}
	glog.Infof("Hairpin mode set to %q", hairpinMode)

	// TODO(#36485) Remove this workaround once we fix the init-container issue.
	// Touch iptables lock file, which will be shared among all processes accessing
	// the iptables.
	f, err := os.OpenFile(utilipt.LockfilePath16x, os.O_CREATE, 0600)
	if err != nil {
		glog.Warningf("Failed to open iptables lock file: %v", err)
	} else if err = f.Close(); err != nil {
		glog.Warningf("Failed to close iptables lock file: %v", err)
	}

	if plug, err := network.InitNetworkPlugin(kubeDeps.NetworkPlugins, crOptions.NetworkPluginName, &criNetworkHost{&networkHost{klet}, &network.NoopPortMappingGetter{}}, hairpinMode, kubeCfg.NonMasqueradeCIDR, int(crOptions.NetworkPluginMTU)); err != nil {
		return nil, err
	} else {
		klet.networkPlugin = plug
	}

	machineInfo, err := klet.GetCachedMachineInfo()
	if err != nil {
		return nil, err
	}

	imageBackOff := flowcontrol.NewBackOff(backOffPeriod, MaxContainerBackOff)

	klet.livenessManager = proberesults.NewManager()

	klet.podCache = kubecontainer.NewCache()
	// podManager is also responsible for keeping secretManager and configMapManager contents up-to-date.
	klet.podManager = kubepod.NewBasicPodManager(kubepod.NewBasicMirrorClient(klet.kubeClient), secretManager, configMapManager)

	if kubeCfg.RemoteRuntimeEndpoint != "" {
		// kubeCfg.RemoteImageEndpoint is same as kubeCfg.RemoteRuntimeEndpoint if not explicitly specified
		if kubeCfg.RemoteImageEndpoint == "" {
			kubeCfg.RemoteImageEndpoint = kubeCfg.RemoteRuntimeEndpoint
		}
	}

	// TODO: These need to become arguments to a standalone docker shim.
	binDir := crOptions.CNIBinDir
	if binDir == "" {
		binDir = crOptions.NetworkPluginDir
	}
	pluginSettings := dockershim.NetworkPluginSettings{
		HairpinMode:       hairpinMode,
		NonMasqueradeCIDR: kubeCfg.NonMasqueradeCIDR,
		PluginName:        crOptions.NetworkPluginName,
		PluginConfDir:     crOptions.CNIConfDir,
		PluginBinDir:      binDir,
		MTU:               int(crOptions.NetworkPluginMTU),
	}

	// Remote runtime shim just cannot talk back to kubelet, so it doesn't
	// support bandwidth shaping or hostports till #35457. To enable legacy
	// features, replace with networkHost.
	var nl *NoOpLegacyHost
	pluginSettings.LegacyRuntimeHost = nl

	// rktnetes cannot be run with CRI.
	if kubeCfg.ContainerRuntime != "rkt" {
		// kubelet defers to the runtime shim to setup networking. Setting
		// this to nil will prevent it from trying to invoke the plugin.
		// It's easier to always probe and initialize plugins till cri
		// becomes the default.
		klet.networkPlugin = nil

		switch kubeCfg.ContainerRuntime {
		case "docker":
			// Create and start the CRI shim running as a grpc server.
			streamingConfig := getStreamingConfig(kubeCfg, kubeDeps)
			ds, err := dockershim.NewDockerService(kubeDeps.DockerClient, kubeCfg.SeccompProfileRoot, crOptions.PodSandboxImage,
				streamingConfig, &pluginSettings, kubeCfg.RuntimeCgroups, kubeCfg.CgroupDriver, crOptions.DockerExecHandlerName,
				crOptions.DockershimRootDirectory, crOptions.DockerDisableSharedPID)
			if err != nil {
				return nil, err
			}
			if err := ds.Start(); err != nil {
				return nil, err
			}
			// For now, the CRI shim redirects the streaming requests to the
			// kubelet, which handles the requests using DockerService..
			klet.criHandler = ds

			// The unix socket for kubelet <-> dockershim communication.
			glog.V(5).Infof("RemoteRuntimeEndpoint: %q, RemoteImageEndpoint: %q",
				kubeCfg.RemoteRuntimeEndpoint,
				kubeCfg.RemoteImageEndpoint)
			glog.V(2).Infof("Starting the GRPC server for the docker CRI shim.")
			server := dockerremote.NewDockerServer(kubeCfg.RemoteRuntimeEndpoint, ds)
			if err := server.Start(); err != nil {
				return nil, err
			}

			// Create dockerLegacyService when the logging driver is not supported.
			supported, err := dockershim.IsCRISupportedLogDriver(kubeDeps.DockerClient)
			if err != nil {
				return nil, err
			}
			if !supported {
				klet.dockerLegacyService = dockershim.NewDockerLegacyService(kubeDeps.DockerClient)
			}
		case "remote":
			// No-op.
			break
		default:
			return nil, fmt.Errorf("unsupported CRI runtime: %q", kubeCfg.ContainerRuntime)
		}
		runtimeService, imageService, err := getRuntimeAndImageServices(kubeCfg)
		if err != nil {
			return nil, err
		}
		runtime, err := kuberuntime.NewKubeGenericRuntimeManager(
			kubecontainer.FilterEventRecorder(kubeDeps.Recorder),
			klet.livenessManager,
			containerRefManager,
			machineInfo,
			klet.podManager,
			kubeDeps.OSInterface,
			klet,
			httpClient,
			imageBackOff,
			kubeCfg.SerializeImagePulls,
			float32(kubeCfg.RegistryPullQPS),
			int(kubeCfg.RegistryBurst),
			kubeCfg.CPUCFSQuota,
			runtimeService,
			imageService,
		)
		if err != nil {
			return nil, err
		}
		klet.containerRuntime = runtime
		klet.runner = runtime
	} else {
		// rkt uses the legacy, non-CRI, integration. Configure it the old way.
		// TODO: Include hairpin mode settings in rkt?
		conf := &rkt.Config{
			Path:            crOptions.RktPath,
			Stage1Image:     crOptions.RktStage1Image,
			InsecureOptions: "image,ondisk",
		}
		runtime, err := rkt.New(
			crOptions.RktAPIEndpoint,
			conf,
			klet,
			kubeDeps.Recorder,
			containerRefManager,
			klet.podManager,
			klet.livenessManager,
			httpClient,
			klet.networkPlugin,
			hairpinMode == componentconfig.HairpinVeth,
			utilexec.New(),
			kubecontainer.RealOS{},
			imageBackOff,
			kubeCfg.SerializeImagePulls,
			float32(kubeCfg.RegistryPullQPS),
			int(kubeCfg.RegistryBurst),
			kubeCfg.RuntimeRequestTimeout.Duration,
		)
		if err != nil {
			return nil, err
		}
		klet.containerRuntime = runtime
		klet.runner = kubecontainer.DirectStreamingRunner(runtime)
	}

	// TODO: Factor out "StatsProvider" from Kubelet so we don't have a cyclic dependency
	klet.resourceAnalyzer = stats.NewResourceAnalyzer(klet, kubeCfg.VolumeStatsAggPeriod.Duration, klet.containerRuntime)

	klet.pleg = pleg.NewGenericPLEG(klet.containerRuntime, plegChannelCapacity, plegRelistPeriod, klet.podCache, clock.RealClock{})
	klet.runtimeState = newRuntimeState(maxWaitForContainerRuntime)
	klet.runtimeState.addHealthCheck("PLEG", klet.pleg.Healthy)
	klet.updatePodCIDR(kubeCfg.PodCIDR)

	// setup containerGC
	containerGC, err := kubecontainer.NewContainerGC(klet.containerRuntime, containerGCPolicy, klet.sourcesReady)
	if err != nil {
		return nil, err
	}
	klet.containerGC = containerGC
	klet.containerDeletor = newPodContainerDeletor(klet.containerRuntime, integer.IntMax(containerGCPolicy.MaxPerPodContainer, minDeadContainerInPod))

	// setup imageManager
	imageManager, err := images.NewImageGCManager(klet.containerRuntime, kubeDeps.CAdvisorInterface, kubeDeps.Recorder, nodeRef, imageGCPolicy)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize image manager: %v", err)
	}
	klet.imageManager = imageManager

	klet.statusManager = status.NewManager(klet.kubeClient, klet.podManager, klet)

	if utilfeature.DefaultFeatureGate.Enabled(features.RotateKubeletServerCertificate) && kubeDeps.TLSOptions != nil {
		var ips []net.IP
		cfgAddress := net.ParseIP(kubeCfg.Address)
		if cfgAddress == nil || cfgAddress.IsUnspecified() {
			if localIPs, err := allLocalIPsWithoutLoopback(); err != nil {
				return nil, err
			} else {
				ips = localIPs
			}
		} else {
			ips = []net.IP{cfgAddress}
		}
		ips = append(ips, cloudIPs...)
		names := append([]string{klet.GetHostname(), hostnameOverride}, cloudNames...)
		klet.serverCertificateManager, err = initializeServerCertificateManager(klet.kubeClient, kubeCfg, klet.nodeName, ips, names)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize certificate manager: %v", err)
		}
		kubeDeps.TLSOptions.Config.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert := klet.serverCertificateManager.Current()
			if cert == nil {
				return nil, fmt.Errorf("no certificate available")
			}
			return cert, nil
		}
	}

	klet.probeManager = prober.NewManager(
		klet.statusManager,
		klet.livenessManager,
		klet.runner,
		containerRefManager,
		kubeDeps.Recorder)

	klet.volumePluginMgr, err =
		NewInitializedVolumePluginMgr(klet, secretManager, configMapManager, kubeDeps.VolumePlugins)
	if err != nil {
		return nil, err
	}

	// If the experimentalMounterPathFlag is set, we do not want to
	// check node capabilities since the mount path is not the default
	if len(kubeCfg.ExperimentalMounterPath) != 0 {
		kubeCfg.ExperimentalCheckNodeCapabilitiesBeforeMount = false
	}
	// setup volumeManager
	klet.volumeManager, err = volumemanager.NewVolumeManager(
		kubeCfg.EnableControllerAttachDetach,
		nodeName,
		klet.podManager,
		klet.statusManager,
		klet.kubeClient,
		klet.volumePluginMgr,
		klet.containerRuntime,
		kubeDeps.Mounter,
		klet.getPodsDir(),
		kubeDeps.Recorder,
		kubeCfg.ExperimentalCheckNodeCapabilitiesBeforeMount,
		kubeCfg.KeepTerminatedPodVolumes)

	runtimeCache, err := kubecontainer.NewRuntimeCache(klet.containerRuntime)
	if err != nil {
		return nil, err
	}
	klet.runtimeCache = runtimeCache
	klet.reasonCache = NewReasonCache()
	klet.workQueue = queue.NewBasicWorkQueue(klet.clock)
	klet.podWorkers = newPodWorkers(klet.syncPod, kubeDeps.Recorder, klet.workQueue, klet.resyncInterval, backOffPeriod, klet.podCache)

	klet.backOff = flowcontrol.NewBackOff(backOffPeriod, MaxContainerBackOff)
	klet.podKillingCh = make(chan *kubecontainer.PodPair, podKillingChannelCapacity)
	klet.setNodeStatusFuncs = klet.defaultNodeStatusFuncs()

	// setup eviction manager
	evictionManager, evictionAdmitHandler := eviction.NewManager(klet.resourceAnalyzer, evictionConfig, killPodNow(klet.podWorkers, kubeDeps.Recorder), klet.imageManager, klet.containerGC, kubeDeps.Recorder, nodeRef, klet.clock)

	klet.evictionManager = evictionManager
	klet.admitHandlers.AddPodAdmitHandler(evictionAdmitHandler)

	// add sysctl admission
	runtimeSupport, err := sysctl.NewRuntimeAdmitHandler(klet.containerRuntime)
	if err != nil {
		return nil, err
	}
	safeWhitelist, err := sysctl.NewWhitelist(sysctl.SafeSysctlWhitelist(), v1.SysctlsPodAnnotationKey)
	if err != nil {
		return nil, err
	}
	// Safe, whitelisted sysctls can always be used as unsafe sysctls in the spec
	// Hence, we concatenate those two lists.
	safeAndUnsafeSysctls := append(sysctl.SafeSysctlWhitelist(), kubeCfg.AllowedUnsafeSysctls...)
	unsafeWhitelist, err := sysctl.NewWhitelist(safeAndUnsafeSysctls, v1.UnsafeSysctlsPodAnnotationKey)
	if err != nil {
		return nil, err
	}
	klet.admitHandlers.AddPodAdmitHandler(runtimeSupport)
	klet.admitHandlers.AddPodAdmitHandler(safeWhitelist)
	klet.admitHandlers.AddPodAdmitHandler(unsafeWhitelist)

	// enable active deadline handler
	activeDeadlineHandler, err := newActiveDeadlineHandler(klet.statusManager, kubeDeps.Recorder, klet.clock)
	if err != nil {
		return nil, err
	}
	klet.AddPodSyncLoopHandler(activeDeadlineHandler)
	klet.AddPodSyncHandler(activeDeadlineHandler)

	criticalPodAdmissionHandler := preemption.NewCriticalPodAdmissionHandler(klet.GetActivePods, killPodNow(klet.podWorkers, kubeDeps.Recorder), kubeDeps.Recorder)
	klet.admitHandlers.AddPodAdmitHandler(lifecycle.NewPredicateAdmitHandler(klet.getNodeAnyWay, criticalPodAdmissionHandler))
	// apply functional Option's
	for _, opt := range kubeDeps.Options {
		opt(klet)
	}

	klet.appArmorValidator = apparmor.NewValidator(kubeCfg.ContainerRuntime)
	klet.softAdmitHandlers.AddPodAdmitHandler(lifecycle.NewAppArmorAdmitHandler(klet.appArmorValidator))
	if utilfeature.DefaultFeatureGate.Enabled(features.Accelerators) {
		if kubeCfg.ContainerRuntime == "docker" {
			if klet.gpuManager, err = nvidia.NewNvidiaGPUManager(klet, kubeDeps.DockerClient); err != nil {
				return nil, err
			}
		} else {
			glog.Errorf("Accelerators feature is supported with docker runtime only. Disabling this feature internally.")
		}
	}
	// Set GPU manager to a stub implementation if it is not enabled or cannot be supported.
	if klet.gpuManager == nil {
		klet.gpuManager = gpu.NewGPUManagerStub()
	}
	// Finally, put the most recent version of the config on the Kubelet, so
	// people can see how it was configured.
	klet.kubeletConfiguration = *kubeCfg
	return klet, nil
}
```

## 7. 第4步骤中调用本步骤进行启动kubelet对象(k8s.io/kubernetes/cmd/kubelet/app/server.go):

```
// 真实的启动kubelet入口
func startKubelet(k kubelet.KubeletBootstrap, podCfg *config.PodConfig, kubeCfg *componentconfig.KubeletConfiguration, kubeDeps *kubelet.KubeletDeps) {
	// start the kubelet
	go wait.Until(func() { k.Run(podCfg.Updates()) }, 0, wait.NeverStop)

	// start the kubelet server
	if kubeCfg.EnableServer {
		go wait.Until(func() {
			k.ListenAndServe(net.ParseIP(kubeCfg.Address), uint(kubeCfg.Port), kubeDeps.TLSOptions, kubeDeps.Auth, kubeCfg.EnableDebuggingHandlers, kubeCfg.EnableContentionProfiling)
		}, 0, wait.NeverStop)
	}
	if kubeCfg.ReadOnlyPort > 0 {
		go wait.Until(func() {
			k.ListenAndServeReadOnly(net.ParseIP(kubeCfg.Address), uint(kubeCfg.ReadOnlyPort))
		}, 0, wait.NeverStop)
	}
}
```

## 8. 上一步骤会调用本步骤运行kubelet对象(k8s.io/kubernetes/pkg/kubelet/kubelet.go):

```
// Run starts the kubelet reacting to config updates
func (kl *Kubelet) Run(updates <-chan kubetypes.PodUpdate) {
	if kl.logServer == nil {
		kl.logServer = http.StripPrefix("/logs/", http.FileServer(http.Dir("/var/log/")))
	}
	if kl.kubeClient == nil {
		glog.Warning("No api server defined - no node status update will be sent.")
	}

	if err := kl.initializeModules(); err != nil {
		kl.recorder.Eventf(kl.nodeRef, v1.EventTypeWarning, events.KubeletSetupFailed, err.Error())
		glog.Error(err)
		kl.runtimeState.setInitError(err)
	}

	// Start volume manager
	go kl.volumeManager.Run(kl.sourcesReady, wait.NeverStop)

	if kl.kubeClient != nil {
		// Start syncing node status immediately, this may set up things the runtime needs to run.
		go wait.Until(kl.syncNodeStatus, kl.nodeStatusUpdateFrequency, wait.NeverStop)
	}
	go wait.Until(kl.syncNetworkStatus, 30*time.Second, wait.NeverStop)
	go wait.Until(kl.updateRuntimeUp, 5*time.Second, wait.NeverStop)

	// Start loop to sync iptables util rules
	if kl.makeIPTablesUtilChains {
		go wait.Until(kl.syncNetworkUtil, 1*time.Minute, wait.NeverStop)
	}

	// Start a goroutine responsible for killing pods (that are not properly
	// handled by pod workers).
	go wait.Until(kl.podKiller, 1*time.Second, wait.NeverStop)

	// Start gorouting responsible for checking limits in resolv.conf
	if kl.resolverConfig != "" {
		go wait.Until(func() { kl.checkLimitsForResolvConf() }, 30*time.Second, wait.NeverStop)
	}

	// Start component sync loops.
	kl.statusManager.Start()
	kl.probeManager.Start()

	// Start the pod lifecycle event generator.
	kl.pleg.Start()
	kl.syncLoop(updates, kl)
}
```