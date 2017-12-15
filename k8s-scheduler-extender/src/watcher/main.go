package main

import (
	"os"
	"os/signal"
	"syscall"

	"watcher/configure"
	"watcher/manager"

	"github.com/golang/glog"
)

func main() {
	// init configure
	configure.InitConfig()

	// init collector manager
	watcherMgr, err := manager.NewWatcherManager()
	if err != nil {
		glog.Error(err)
		return
	}

	// run collector manager
	err = watcherMgr.Run()
	if err != nil {
		glog.Error(err)
		return
	}

	// init exit
	exitChan := make(chan int)
	signalChan := make(chan os.Signal, 1)
	go func() {
		<-signalChan
		exitChan <- 1
	}()
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// main groutine wait for exit
	<-exitChan

	// disk collector stop
	watcherMgr.Stop()
}
