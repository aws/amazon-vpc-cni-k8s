// There is no golang code here.  This file exists only to hook the
// following command into "go generate ./..."

package manifests

//go:generate go run github.com/google/go-jsonnet/cmd/jsonnet -S -m . manifests.jsonnet
