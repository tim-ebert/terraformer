# SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

#############      golang-base   #############
FROM golang:1.15.3 AS golang-base

#############      base          #############
FROM golang-base AS base

WORKDIR /tmp/terraformer
COPY TF_VERSION .

RUN export TF_VERSION=$(cat /tmp/terraformer/TF_VERSION) && \
    apt-get update && \
    apt-get install -y unzip && \
    # install terraform and needed provider plugins
    mkdir -p /go/src/github.com/hashicorp && \
    git clone --single-branch --depth 1 --branch v${TF_VERSION} https://github.com/hashicorp/terraform.git /go/src/github.com/hashicorp/terraform && \
    cd /go/src/github.com/hashicorp/terraform && \
    go install ./tools/terraform-bundle

COPY hack hack
COPY terraform-bundle.hcl .
RUN ./hack/fetch-providers.sh

#############      builder       #############
FROM golang-base AS builder

WORKDIR /go/src/github.com/gardener/terraformer
COPY . .

RUN make install

#############   terraformer      #############
FROM alpine:3.12.1 AS terraformer

RUN apk add --update bash curl tzdata

WORKDIR /

ENV TF_DEV=true
ENV TF_RELEASE=true

COPY --from=base /tmp/terraformer/terraform /bin/terraform
COPY --from=base /tmp/terraformer/terraform-provider* /terraform-providers/
COPY --from=builder /go/bin/terraformer /

ENTRYPOINT /terraformer

#############      dev           #############
FROM golang-base AS dev

WORKDIR /go/src/github.com/gardener/terraformer
VOLUME /go/src/github.com/gardener/terraformer

COPY --from=base /tmp/terraformer/terraform /bin/terraform
COPY --from=base /tmp/terraformer/terraform-provider* /terraform-providers/

COPY vendor vendor
COPY Makefile VERSION go.mod go.sum ./

RUN make install-requirements
