FROM public.ecr.aws/bitnami/golang:1.13 as builder

WORKDIR /workspace
ENV GOPROXY direct

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY cmd cmd
COPY pkg pkg

# Package all testing binaries into one docker file
# which can be used for different test scenarios

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build \
    -a -o traffic-server cmd/traffic-server/main.go

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build \
    -a -o traffic-client cmd/traffic-client/main.go

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build \
    -a -o networking cmd/networking/main.go

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build \
    -a -o metric-server cmd/metric-server/main.go

FROM public.ecr.aws/amazonlinux/amazonlinux:2
RUN yum update -y && \
    yum clean all

WORKDIR /
COPY --from=builder /workspace/ .
