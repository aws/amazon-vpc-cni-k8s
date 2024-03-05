ARG golang_image

FROM $golang_image as builder

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

FROM public.ecr.aws/eks-distro-build-tooling/eks-distro-minimal-base-iptables:latest.2

WORKDIR /
COPY --from=builder /workspace/ .
