#############      builder       #############
FROM golang:1.12.13 AS builder

WORKDIR /tmp/terraformer
COPY . .

RUN export TF_VERSION=$(cat /tmp/terraformer/TF_VERSION) && \
    export KUBECTL_VERSION=$(cat /tmp/terraformer/KUBECTL_VERSION) && \
    apt-get update && \
    apt-get install -y unzip && \
    # install terraform and needed provider plugins
    mkdir -p /go/src/github.com/hashicorp && \
    git clone --single-branch --depth 1 --branch v${TF_VERSION} https://github.com/hashicorp/terraform.git /go/src/github.com/hashicorp/terraform && \
    cd /go/src/github.com/hashicorp/terraform && \
    go install ./tools/terraform-bundle && \
    cd /tmp/terraformer && \
    ./scripts/fetch-providers && \
    # install kubectl binary
    curl -LO https://storage.googleapis.com/kubernetes-release/release/v${KUBECTL_VERSION}/bin/linux/amd64/kubectl && \
    chmod +x ./kubectl

#############   terraformer      #############
FROM alpine:3.10.3 AS base

RUN apk add --update bash curl tzdata

WORKDIR /

ENV TF_DEV=true
ENV TF_RELEASE=true

COPY --from=builder /tmp/terraformer/kubectl /bin/kubectl
COPY --from=builder /tmp/terraformer/terraform /bin/terraform
COPY --from=builder /tmp/terraformer/terraform-provider* /terraform-providers/

ADD ./terraform.sh /terraform.sh

CMD exec /terraform.sh
