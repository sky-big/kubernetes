# kubernetes compile

1.hack目录是整个k8s编译测试集群等的shell脚本存放区(cluster目录下面是跟集群相关的shell脚本存放区，但是整个shell脚本的log打印函数在cluster/lib目录下面)

    hack/lib                            -- 整个k8s的编译测试集群等基础脚本存放区
    cluster/lib/logging.sh              -- 整个k8s编译测试集群等相关shell脚本的log打印shell脚本
    hack/make-rules/build.sh            -- 编译的起始脚本

    编译k8s某个功能模块的流程(比如scheduler)：Makefile -> Makefile.generated_files -> hack/make-rules/build.sh -> hack/lib

2.部分编译k8s的命令是:  

    make kube-scheduler
    
