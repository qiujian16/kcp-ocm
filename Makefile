SHELL :=/bin/bash

all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	lib/tmp.mk \
)

# Image URL to use all building/pushing image targets;
IMAGE ?= kcp-acm-integration-controller
IMAGE_TAG?=latest
IMAGE_REGISTRY ?= quay.io/skeeey
IMAGE_NAME?=$(IMAGE_REGISTRY)/$(IMAGE):$(IMAGE_TAG)
KUBECTL?=kubectl

GIT_HOST ?= github.com/qiujian16/kcp-ocm
BASE_DIR := $(shell basename $(PWD))
DEST := $(GOPATH)/src/$(GIT_HOST)/$(BASE_DIR)

# Add packages to do unit test
GO_TEST_PACKAGES :=./pkg/...

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
# It will generate target "image-$(1)" for building the image and binding it as a prerequisite to target "images".
$(call build-image,$(IMAGE),$(IMAGE_REGISTRY)/$(IMAGE),./Dockerfile,.)

deploy:
	$(KUBECTL) -n open-cluster-management delete secret kcp-admin-kubeconfig --ignore-not-found --kubeconfig $(HUB_KUBECONFIG)
	$(KUBECTL) -n open-cluster-management create secret generic kcp-admin-kubeconfig --from-file=admin.kubeconfig=$(KCP_KUBECONFIG) --kubeconfig $(HUB_KUBECONFIG)
	$(KUBECTL) apply -k deploy/base --kubeconfig $(HUB_KUBECONFIG)

deploy-with-client-ca:
	$(KUBECTL) -n open-cluster-management delete secret kcp-admin-kubeconfig --ignore-not-found --kubeconfig $(HUB_KUBECONFIG)
	$(KUBECTL) -n open-cluster-management create secret generic kcp-admin-kubeconfig --from-file=admin.kubeconfig=$(KCP_KUBECONFIG) --kubeconfig $(HUB_KUBECONFIG)
	$(KUBECTL) -n open-cluster-management delete secret kcp-client-ca --ignore-not-found --kubeconfig $(HUB_KUBECONFIG)
	$(KUBECTL) -n open-cluster-management create secret generic kcp-client-ca --from-file=rootca.crt=${CLIENT_CA_FILE} --from-file=rootca.key=${CLIENT_CA_KEY_FILE} --kubeconfig $(HUB_KUBECONFIG)
	$(KUBECTL) apply -k deploy/client-ca --kubeconfig $(HUB_KUBECONFIG)
