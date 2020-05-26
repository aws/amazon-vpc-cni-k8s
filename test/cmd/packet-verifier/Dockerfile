ARG builder_image=amazonlinux:2
 
FROM $builder_image as builder
ENV GO111MODULE=on
ENV GOPROXY=direct
ENV GOOS=linux
ENV GOARCH=amd64
COPY . /
WORKDIR /
RUN yum install -y git golang libpcap-devel
RUN go build -o packet-verifier packet-verifier.go
 
FROM amazonlinux:2
RUN yum install -y libpcap-devel
COPY --from=builder packet-verifier /usr/bin/packet-verifier
