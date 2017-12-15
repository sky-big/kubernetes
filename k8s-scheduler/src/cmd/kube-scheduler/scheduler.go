/*
Copyright 2014 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"k8s.io/apiserver/pkg/util/flag"
	"k8s.io/apiserver/pkg/util/logs"
	"k8s.io/kubernetes/pkg/version/verflag"
	"k8s.io/kubernetes/plugin/cmd/kube-scheduler/app"
	"k8s.io/kubernetes/plugin/cmd/kube-scheduler/app/options"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
)

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
