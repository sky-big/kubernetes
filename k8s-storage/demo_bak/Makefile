GOPATH:= $(CURDIR)/vendor:$(GOPATH)
.PHONY: all
all: build

.PHONY: build
build:
	gb build control

.PHONY: dep
dep:
	gb vendor restore

.PHONY: clean
clean:
	rm pkg/* -rf
	rm bin/*
