FROM golang:1.12-stretch
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
