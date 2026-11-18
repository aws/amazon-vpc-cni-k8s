// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package utils

import (
	"os"
	"path/filepath"
	"strings"

	appsV1 "k8s.io/api/apps/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// NamespacedName returns the namespaced name for k8s objects
func NamespacedName(obj v1.Object) types.NamespacedName {
	return types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

func GetEnvValueForKeyFromDaemonSet(key string, ds *appsV1.DaemonSet) string {
	envVar := ds.Spec.Template.Spec.Containers[0].Env
	for _, env := range envVar {
		if env.Name == key {
			return env.Value
		}
	}
	return ""
}

// moduleRoot is the module path declared in the repository's root go.mod.
const moduleRoot = "github.com/aws/amazon-vpc-cni-k8s"

// GetProjectRoot returns the root of the amazon-vpc-cni-k8s checkout that the running test binary was built in.
func GetProjectRoot() string {
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	return projectRootFrom(dir)
}

// projectRootFrom walks up from dir to the directory whose go.mod declares moduleRoot. That works whatever
// the checkout directory is called, including paths where "vpc-cni" also appears in a parent directory
// name (for example a Hydra workspace such as /workspace/<acct>-Hydra-vpc-cni-release-<id>/repository/vpc-cni).
// If no such go.mod is found, it falls back to the previous name-based lookup.
func projectRootFrom(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if goMod, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			for _, line := range strings.Split(string(goMod), "\n") {
				if strings.TrimSpace(line) == "module "+moduleRoot {
					return d
				}
			}
		}
		if parent := filepath.Dir(d); parent == d {
			break
		}
	}

	projectRoot := strings.SplitAfter(dir, "amazon-vpc-cni-k8s")[0]
	if dir == projectRoot {
		// in prow tests, the repository name is "vpc-cni"
		projectRoot = strings.SplitAfter(dir, "vpc-cni")[0]
	}
	return projectRoot
}
