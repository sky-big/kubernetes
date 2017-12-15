GOPATH:= $(CURDIR)/vendor:$(GOPATH)

all: build
.PHONY: all

build:
	gb build scheduler
	gb build watcher
.PHONY: build

dep:
	gb vendor restore
.PHONY: dep

run-client-go:
	sudo ./bin/client-go --kubeconfig=/home/sky-big/.kube/config
.PHONY: run-client-go

get-etcd:
	etcdctl get /registry/vessel.k8s.io/disks/default/disk
.PHONY: get-etcd

clean:
	rm pkg/* -rf
	rm bin/*
.PHONY: clean

