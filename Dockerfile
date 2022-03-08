FROM quay.io/bitnami/golang:1.17 AS builder
WORKDIR /go/src/github.com/qiujian16/kcp-ocm
COPY . .
ENV GO_PACKAGE github.com/qiujian16/kcp-ocm

RUN make build --warn-undefined-variables

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

COPY --from=builder /go/src/github.com/qiujian16/kcp-ocm/kcp-ocm /
RUN microdnf update && microdnf clean all
USER 65532:65532
