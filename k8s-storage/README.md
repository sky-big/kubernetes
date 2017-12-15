kubernetes storage study

1.目前测试local storage volume plugin需要做以下一些操作:  

    (1).需要k8s-v1.8-alpha.1这个版本的k8s

    (2).需要将pkg/features/kube_features.go文件中将这个本地存储的插件在k8s中打开使用(目前这个插件是刚开发的，默认是处于关闭状态),操作就是修改代码如下：
    var defaultKubernetesFeatureGates = map[utilfeature.Feature]utilfeature.FeatureSpec{
        ExternalTrafficLocalOnly:                    {Default: true, PreRelease: utilfeature.GA},
        AppArmor:                                    {Default: true, PreRelease: utilfeature.Beta},
        DynamicKubeletConfig:                        {Default: false, PreRelease: utilfeature.Alpha},
        DynamicVolumeProvisioning:                   {Default: true, PreRelease: utilfeature.Alpha},
        ExperimentalHostUserNamespaceDefaultingGate: {Default: false, PreRelease: utilfeature.Beta},
        ExperimentalCriticalPodAnnotation:           {Default: false, PreRelease: utilfeature.Alpha},
        AffinityInAnnotations:                       {Default: false, PreRelease: utilfeature.Alpha},
        Accelerators:                                {Default: false, PreRelease: utilfeature.Alpha},
        TaintBasedEvictions:                         {Default: false, PreRelease: utilfeature.Alpha},
        RotateKubeletServerCertificate:              {Default: false, PreRelease: utilfeature.Alpha},
        RotateKubeletClientCertificate:              {Default: false, PreRelease: utilfeature.Alpha},
        PersistentLocalVolumes:                      {Default: true, PreRelease: utilfeature.Alpha},    // 即修改这一行，将默认的false修改为true
        LocalStorageCapacityIsolation:               {Default: false, PreRelease: utilfeature.Alpha},
    }

    该问题通过询问作者已经得到解决，不需要删除代码也能够进行创建，这样创建的PV带有节点亲和性的东西，在调度的时候会考虑节点的亲和性
    (3).目前需要将PV检查的时候，将本地存储这个插件的节点亲和性检查的代码删除,即pkg/api/validation/validation.go文件中的代码  
        删除ValidatePersistentVolume函数中的代码即：
        1218行:
        nodeAffinitySpecified, errs := validateStorageNodeAffinityAnnotation(pv.ObjectMeta.Annotations, metaPath.Child("annotations"))
        allErrs = append(allErrs, errs...)

        1382行:
        // NodeAffinity is required
        if !nodeAffinitySpecified {
                            allErrs = append(allErrs, field.Required(metaPath.Child("annotations"), "Local volume requires node affinity"))
                                        
        }

    (4).然后编译k8s的代码，将组件拷贝到操作系统，然后重启k8s的组件即可

    (5).然后可以去启动yaml目录中的PV，PVC等进行测试观察

