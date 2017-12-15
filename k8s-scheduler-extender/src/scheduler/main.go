package main

import (
	"os"
	"os/signal"
	"syscall"

	"scheduler/configure"
	"scheduler/httpserver"

	"github.com/golang/glog"
)

func main() {
	// init configure
	configure.InitConfig()

	// new http server
	httpServer, err := httpserver.NewHttpServer()
	if err != nil {
		glog.Error(err)
		return
	}
	// run http server
	if err := httpServer.Run(); err != nil {
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

	// stop http server
	httpServer.Stop()
}
