# Copyright (c) 2017 SAP SE or an SAP affiliate company. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

NAME                 := terraformer
IMAGE_REPOSITORY     := eu.gcr.io/gardener-project/gardener/$(NAME)
IMAGE_REPOSITORY_DEV := $(IMAGE_REPOSITORY)/dev
REPO_ROOT            := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
VERSION              := $(shell cat "$(REPO_ROOT)/VERSION")
IMAGE_TAG            := $(VERSION)

#########################################
# Rules for local development scenarios #
#########################################

COMMAND := apply
ZAP_DEVEL := true
.PHONY: run
run:
	# running `go run ./cmd/terraformer $(COMMAND)`
	go run ./cmd/terraformer $(COMMAND) \
       --zap-devel=$(ZAP_DEVEL) \
       --configuration-configmap-name=example.infra.tf-config \
       --state-configmap-name=example.infra.tf-state \
       --variables-secret-name=example.infra.tf-vars

.PHONY: start
start: dev-kubeconfig docker-dev-image
	# starting dev container
	@docker run -it -v $(shell go env GOCACHE):/root/.cache/go-build \
       -v $(REPO_ROOT):/go/src/github.com/gardener/terraformer \
       -e KUBECONFIG=/go/src/github.com/gardener/terraformer/dev/kubeconfig.yaml \
       -e NAMESPACE=${NAMESPACE} \
       --name terraformer-dev --rm \
       $(IMAGE_REPOSITORY_DEV):$(IMAGE_TAG) \
       make run COMMAND=$(COMMAND) ZAP_DEVEL=$(ZAP_DEVEL)

.PHONY: start-dev-container
start-dev-container: dev-kubeconfig docker-dev-image
	# starting dev container
	@docker run -it -v $(shell go env GOCACHE):/root/.cache/go-build \
       -v $(REPO_ROOT):/go/src/github.com/gardener/terraformer \
       -e KUBECONFIG=/go/src/github.com/gardener/terraformer/dev/kubeconfig.yaml \
       -e NAMESPACE=${NAMESPACE} \
       --name terraformer-dev --rm \
       $(IMAGE_REPOSITORY_DEV):$(IMAGE_TAG) \
       bash

.PHONY: docker-dev-image
docker-dev-image:
	@DOCKER_BUILDKIT=1 docker build -t $(IMAGE_REPOSITORY_DEV):$(IMAGE_TAG) --target dev --build-arg BUILDKIT_INLINE_CACHE=1 .

.PHONY: dev-kubeconfig
dev-kubeconfig:
	@mkdir -p dev
	@kubectl config view --raw | sed 's/localhost/host.docker.internal/' > dev/kubeconfig.yaml

#################################################################
# Rules related to binary build, Docker image build and release #
#################################################################

.PHONY: install
install:
	@LD_FLAGS="-w -X github.com/gardener/$(NAME)/pkg/version.Version=$(VERSION)" \
		$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/install.sh ./cmd/...

.PHONY: build
build: docker-image bundle-clean

.PHONY: release
release: build docker-login docker-push

.PHONY: docker-image
docker-image:
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
	@go install -mod=vendor $(REPO_ROOT)/vendor/github.com/onsi/ginkgo/ginkgo
	@GO111MODULE=off go get golang.org/x/tools/cmd/goimports

.PHONY: revendor
revendor:
	@GO111MODULE=on go mod vendor
	@GO111MODULE=on go mod tidy
	@chmod +x $(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/*
	@chmod +x $(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/.ci/*
	@$(REPO_ROOT)/hack/update-github-templates.sh

.PHONY: clean
clean: bundle-clean
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/clean.sh ./cmd/... ./pkg/...

.PHONY: check-generate
check-generate:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/check-generate.sh $(REPO_ROOT)

.PHONY: check
check:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/check.sh --golangci-lint-config=./.golangci.yaml ./cmd/... ./pkg/...

.PHONY: generate
generate:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/generate.sh ./cmd/... ./pkg/...

.PHONY: format
format:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/format.sh ./cmd ./pkg

.PHONY: test
test:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/test.sh -r ./cmd/... ./pkg/...

.PHONY: test-cov
test-cov:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/test-cover.sh -r ./cmd/... ./pkg/...

.PHONY: test-clean
test-clean:
	@$(REPO_ROOT)/vendor/github.com/gardener/gardener/hack/test-cover-clean.sh

.PHONY: verify
verify: check format test

.PHONY: verify-extended
verify-extended: install-requirements check-generate check format test-cov test-clean
