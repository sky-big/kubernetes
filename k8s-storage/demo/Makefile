ifeq ($(REGISTRY),)
	REGISTRY = 192.168.152.134:5000/
endif

ifeq ($(VERSION),)
	VERSION = latest
endif

IMAGE = $(REGISTRY)provisioner:$(VERSION)
MUTABLE_IMAGE = $(REGISTRY)provisioner:latest

all build:
	gb build provisioner
.PHONY: all build

container: build quick-container
.PHONY: container

quick-container:
	cp provisioner deployment/docker
	docker build -t $(MUTABLE_IMAGE) deployment/docker
	docker tag $(MUTABLE_IMAGE) $(IMAGE)
.PHONY: quick-container

push: container
	docker push $(IMAGE)
	docker push $(MUTABLE_IMAGE)
.PHONY: push

test:
.PHONY: test

clean:
	rm -f provisioner
	rm -f deployment/docker/provisioner
.PHONY: clean
