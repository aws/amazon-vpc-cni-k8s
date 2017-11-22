SOURCEDIR=./pkg ./plugins
SOURCES := $(shell find $(SOURCEDIR) -name '*.go')
ROOT := $(shell pwd)
LOCAL_ENI_PLUGIN_BINARY=bin/plugins/ecs-eni
LOCAL_IPAM_PLUGIN_BINARY=bin/plugins/ecs-ipam
LOCAL_BRIDGE_PLUGIN_BINARY=bin/plugins/ecs-bridge
VERSION=$(shell cat $(ROOT)/VERSION)
GO_EXECUTABLE=$(shell command -v go 2> /dev/null)

# We want to embed some git details in the build. We support pulling
# these details from the environment in order support builds outside
# of a git working copy. If they're not given explicitly, we'll try to
# use git to directly inspect the repository state...
GIT_SHORT_HASH ?= $(shell git rev-parse --short HEAD 2> /dev/null)
GIT_PORCELAIN ?= $(shell git status --porcelain 2> /dev/null | wc -l)

# ...and if we can't inspect the repo state, we'll fall back to some
# static strings
ifeq ($(strip $(GIT_SHORT_HASH)),)
  GIT_SHORT_HASH=unknown
endif
ifeq ($(strip $(GIT_PORCELAIN)),)
  # This indicates that the repo is dirty. Since we can't tell whether
  # it is or not, this seems the safest fallback.
  GIT_PORCELAIN=1
endif

.PHONY: get-deps

get-deps:
	go get github.com/golang/mock/gomock
	go get github.com/golang/mock/mockgen
	go get golang.org/x/tools/cmd/goimports
	go get github.com/tools/godep

.PHONY: plugins
plugins: $(LOCAL_ENI_PLUGIN_BINARY) $(LOCAL_IPAM_PLUGIN_BINARY) $(LOCAL_BRIDGE_PLUGIN_BINARY)

$(LOCAL_ENI_PLUGIN_BINARY): $(SOURCES)
	GOOS=linux CGO_ENABLED=0 go build -installsuffix cgo -a -ldflags "\
	     -X github.com/aws/amazon-ecs-cni-plugins/pkg/version.GitShortHash=$(GIT_SHORT_HASH) \
	     -X github.com/aws/amazon-ecs-cni-plugins/pkg/version.GitPorcelain=$(GIT_PORCELAIN) \
	     -X github.com/aws/amazon-ecs-cni-plugins/pkg/version.Version=$(VERSION) -s" \
	     -o ${ROOT}/${LOCAL_ENI_PLUGIN_BINARY} github.com/aws/amazon-ecs-cni-plugins/plugins/eni
	@echo "Built eni plugin"

$(LOCAL_IPAM_PLUGIN_BINARY): $(SOURCES)
	GOOS=linux CGO_ENABLED=0 go build -installsuffix cgo -a -ldflags "\
	     -X github.com/aws/amazon-ecs-cni-plugins/pkg/version.GitShortHash=$(GIT_SHORT_HASH) \
	     -X github.com/aws/amazon-ecs-cni-plugins/pkg/version.GitPorcelain=$(GIT_PORCELAIN) \
	     -X github.com/aws/amazon-ecs-cni-plugins/pkg/version.Version=$(VERSION) -s" \
	     -o ${ROOT}/${LOCAL_IPAM_PLUGIN_BINARY} github.com/aws/amazon-ecs-cni-plugins/plugins/ipam
	@echo "Built ipam plugin"

$(LOCAL_BRIDGE_PLUGIN_BINARY): $(SOURCES)
	GOOS=linux CGO_ENABLED=0 go build -installsuffix cgo -a -ldflags "\
	     -X github.com/aws/amazon-ecs-cni-plugins/pkg/version.GitShortHash=$(GIT_SHORT_HASH) \
	     -X github.com/aws/amazon-ecs-cni-plugins/pkg/version.GitPorcelain=$(GIT_PORCELAIN) \
	     -X github.com/aws/amazon-ecs-cni-plugins/pkg/version.Version=$(VERSION) -s" \
	     -o ${ROOT}/${LOCAL_BRIDGE_PLUGIN_BINARY} github.com/aws/amazon-ecs-cni-plugins/plugins/ecs-bridge
	@echo "Built bridge plugin"

.PHONY: generate
generate: $(SOURCES)
	go generate -x ./pkg/... ./plugins/...

.PHONY: unit-test integration-test e2e-test
unit-test: $(SOURCES)
	go test -v -cover -race -timeout 10s ./pkg/... ./plugins/...

integration-test: $(SOURCE)
	go test -v -tags integration -race -timeout 10s ./pkg/... ./plugins/...

e2e-test:  $(SOURCE) plugins
	sudo -E CNI_PATH=${ROOT}/bin/plugins ${GO_EXECUTABLE} test -v -tags e2e -race -timeout 60s ./plugins/...

.PHONY: clean
clean:
	rm -rf ${ROOT}/bin ||:
