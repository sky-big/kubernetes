GOPATH:= $(CURDIR)/vendor:$(GOPATH)

.PHONY: all
all: build

.PHONY: build
build:
	gb build cmd/kube-scheduler

.PHONY: test
test:

.PHONY: dep
dep:
	gb vendor restore

.PHONY: clean
clean:
	rm pkg/* -rf
	rm bin/*
