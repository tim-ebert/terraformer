# SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

NAME                 := terraformer
IMAGE_REPOSITORY     := eu.gcr.io/gardener-project/gardener/$(NAME)
IMAGE_REPOSITORY_DEV := $(IMAGE_REPOSITORY)/dev
REPO_ROOT            := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
VERSION              := $(shell cat "$(REPO_ROOT)/VERSION")
EFFECTIVE_VERSION    := $(VERSION)-$(shell git rev-parse HEAD)

ifneq ($(strip $(shell git status --porcelain 2>/dev/null)),)
	EFFECTIVE_VERSION := $(EFFECTIVE_VERSION)-dirty
endif

IMAGE_TAG            := $(EFFECTIVE_VERSION)
LD_FLAGS             := "-w -X github.com/gardener/$(NAME)/pkg/version.Version=$(EFFECTIVE_VERSION)"

#########################################
# Rules for local development scenarios #
#########################################

COMMAND       := apply
ZAP_DEVEL     := true
ZAP_LOG_LEVEL := debug
.PHONY: run
run:
	# running `go run ./cmd/terraformer $(COMMAND)`
	go run -ldflags $(LD_FLAGS) -mod=vendor \
		./cmd/terraformer $(COMMAND) \
		--zap-devel=$(ZAP_DEVEL) \
		--zap-log-level=$(ZAP_LOG_LEVEL) \
		--configuration-configmap-name=example.infra.tf-config \
		--state-configmap-name=example.infra.tf-state \
		--variables-secret-name=example.infra.tf-vars

.PHONY: start
start: dev-kubeconfig docker-dev-image
	@docker run -it -v $(shell go env GOCACHE):/root/.cache/go-build \
		-v $(REPO_ROOT):/go/src/github.com/gardener/terraformer \
		-e KUBECONFIG=/go/src/github.com/gardener/terraformer/dev/kubeconfig.yaml \
		-e NAMESPACE=${NAMESPACE} \
		--name terraformer-dev --rm \
		$(IMAGE_REPOSITORY_DEV):$(VERSION) \
		make run COMMAND=$(COMMAND) ZAP_DEVEL=$(ZAP_DEVEL) ZAP_LOG_LEVEL=$(ZAP_LOG_LEVEL)

.PHONY: start-dev-container
start-dev-container: dev-kubeconfig docker-dev-image
	# starting dev container
	@docker run -it -v $(shell go env GOCACHE):/root/.cache/go-build \
		-v $(REPO_ROOT):/go/src/github.com/gardener/terraformer \
		-v $(REPO_ROOT)/bin/container:/go/src/github.com/gardener/terraformer/bin \
		-e KUBEBUILDER_ASSETS=/go/src/github.com/gardener/terraformer/bin/kubebuilder/bin \
		-e KUBECONFIG=/go/src/github.com/gardener/terraformer/dev/kubeconfig.yaml \
		-e NAMESPACE=${NAMESPACE} \
		--name terraformer-dev --rm \
		$(IMAGE_REPOSITORY_DEV):$(VERSION) \
		bash

.PHONY: docker-dev-image
docker-dev-image:
	@DOCKER_BUILDKIT=1 docker build -t $(IMAGE_REPOSITORY_DEV):$(VERSION) --target dev --build-arg BUILDKIT_INLINE_CACHE=1 .

.PHONY: dev-kubeconfig
dev-kubeconfig:
	@mkdir -p dev
	@kubectl config view --raw | sed -E 's/127.0.0.1|localhost/host.docker.internal/' > dev/kubeconfig.yaml

#################################################################
# Rules related to binary build, Docker image build and release #
#################################################################

.PHONY: install
install:
	@LD_FLAGS=$(LD_FLAGS) $(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/install.sh ./cmd/terraformer...

.PHONY: build
build: docker-image bundle-clean

.PHONY: release
release: build docker-login docker-push

.PHONY: docker-image
docker-image:
	# building docker image with tag $(IMAGE_REPOSITORY):$(IMAGE_TAG)
	@docker build -t $(IMAGE_REPOSITORY):$(IMAGE_TAG) --rm --target terraformer .

.PHONY: docker-login
docker-login:
	@gcloud auth activate-service-account --key-file .kube-secrets/gcr/gcr-readwrite.json

.PHONY: docker-push
docker-push:
	@if ! docker images $(IMAGE_REPOSITORY) | awk '{ print $$2 }' | grep -q -F $(IMAGE_TAG); then echo "$(IMAGE_REPOSITORY) version $(IMAGE_TAG) is not yet built. Please run 'make docker-image'"; false; fi
	@gcloud docker -- push $(IMAGE_REPOSITORY):$(IMAGE_TAG)

.PHONY: bundle-clean
bundle-clean:
	@rm -f terraform-provider*
	@rm -f terraform
	@rm -f terraform*.zip
	@rm -rf bin/

#####################################################################
# Rules for verification, formatting, linting, testing and cleaning #
#####################################################################

.PHONY: install-requirements
install-requirements:
	@go install -mod=vendor github.com/onsi/ginkgo/ginkgo
	@go install -mod=vendor github.com/golang/mock/mockgen
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/install-requirements.sh

.PHONY: revendor
revendor:
	@GO111MODULE=on go mod vendor
	@GO111MODULE=on go mod tidy
	@chmod +x $(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/*
	@chmod +x $(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/.ci/*
	@$(REPO_ROOT)/hack/update-github-templates.sh

.PHONY: clean
clean: bundle-clean
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/clean.sh ./cmd/... ./pkg/... ./test/...

.PHONY: check-generate
check-generate:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/check-generate.sh $(REPO_ROOT)

.PHONY: check
check:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/check.sh --golangci-lint-config=./.golangci.yaml ./cmd/... ./pkg/... ./test/...

.PHONY: generate
generate:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/generate.sh ./cmd/... ./pkg/... ./test/...

.PHONY: format
format:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/format.sh ./cmd ./pkg ./test

.PHONY: test
test:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/test.sh ./cmd/... ./pkg/... ./test/e2e/binary/...

.PHONY: test-cov
test-cov:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/test-cover.sh ./cmd/... ./pkg/... ./test/e2e/binary/...

.PHONY: test-cov-clean
test-cov-clean:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/test-cover-clean.sh

.PHONY: verify
verify: check format test

.PHONY: verify-extended
verify-extended: install-requirements check-generate check format test-cov test-cov-clean
