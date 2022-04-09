FROM public.ecr.aws/docker/library/golang:1.17-stretch as builder

WORKDIR /workspace
ENV GOPROXY direct

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY cmd cmd
COPY pkg pkg

# Package all testing binaries into one docker file
# which can be used for different test scenarios

RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build \
    -a -o traffic-server cmd/traffic-server/main.go

RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build \
    -a -o traffic-client cmd/traffic-client/main.go

RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build \
    -a -o networking cmd/networking/main.go

RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build \
    -a -o metric-server cmd/metric-server/main.go

RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build \
    -a -o snat-utils cmd/snat-utils/main.go

FROM public.ecr.aws/amazonlinux/amazonlinux:2
RUN yum update -y && \
    yum install -y iptables && \
    yum clean all

WORKDIR /
COPY --from=builder /workspace/ .
