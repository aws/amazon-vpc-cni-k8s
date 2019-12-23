FROM golang:1.12-stretch as builder
WORKDIR /go/src/github.com/aws/amazon-vpc-cni-k8s

ARG arch
ENV ARCH=$arch

# Force the go compiler to use modules.
ENV GO111MODULE=on

# go.mod and go.sum go into their own layers.
COPY go.mod .
COPY go.sum .

# This ensures `go mod download` happens only when go.mod and go.sum change.
RUN go mod download

COPY . .
RUN make build-metrics

FROM amazonlinux:2
RUN yum update -y && \
    yum clean all

WORKDIR /app

COPY --from=builder /go/src/github.com/aws/amazon-vpc-cni-k8s/cni-metrics-helper/cni-metrics-helper /app

# Copy our bundled certs to the first place go will check: see
# https://golang.org/src/pkg/crypto/x509/root_unix.go
COPY --from=builder /go/src/github.com/aws/amazon-vpc-cni-k8s/misc/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ENTRYPOINT /app/cni-metrics-helper --cloudwatch=false
