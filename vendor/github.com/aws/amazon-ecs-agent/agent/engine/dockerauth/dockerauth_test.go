// +build !integration
// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package dockerauth

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	docker "github.com/fsouza/go-dockerclient"
)

type authTestPair struct {
	Image        string
	ExpectedUser string
	ExpectedPass string
}

var secretAuth string = base64.StdEncoding.EncodeToString([]byte("user:swordfish"))
var dragonAuth string = base64.StdEncoding.EncodeToString([]byte("test:dragon"))
var dockerhubAuth string = base64.StdEncoding.EncodeToString([]byte("dockerhub:password"))

func TestDockerCfgAuth(t *testing.T) {
	var expectedPairs = []authTestPair{
		{"example.tld/my/image", "user", "swordfish"},
		{"example.tld/user2/image", "test", "dragon"},
		{"registry.tld/image", "", ""},
		{"nginx", "dockerhub", "password"},
		{"busybox", "dockerhub", "password"},
		{"library/busybox", "dockerhub", "password"},
		{"amazon/amazon-ecs-agent", "dockerhub", "password"},
		{"aregistry.tld/foo/bar", "user", "swordfish"},
		{"aregistry.tld/foo", "user", "swordfish"},
		{"aregistry.tld2/foo", "", ""},
		{"foo.aregistry.tld/foo", "", ""},
		{"anotherregistry.tld/foo/bar", "user", "swordfish"},
		{"anotherregistry.tld/foo", "user", "swordfish"},
		{"anotherregistry.tld2/foo", "", ""},
		{"foo.anotherregistry.tld/foo", "", ""},
	}
	authData := []byte(strings.Join(append([]string{`{`},
		`"example.tld/user2":{"auth":"`+dragonAuth+`","email":"test@test.test"},`,
		`"example.tld":{"auth":"`+secretAuth+`","email":"user2@example.com"},`,
		`"https://aregistry.tld":{"auth":"`+secretAuth+`"},`,
		`"http://anotherregistry.tld":{"auth":"`+secretAuth+`"},`,
		`"https://index.docker.io/v1/":{"auth":"`+dockerhubAuth+`"}`,
		`}`), ""))
	dockerAuthData := []byte(strings.Join(append([]string{`{`},
		`"example.tld/user2":{"username":"test","password":"dragon","email":"foo@bar"},`,
		`"example.tld":{"username":"user","password":"swordfish"},`,
		`"https://aregistry.tld":{"username":"user","password":"swordfish"},`,
		`"http://anotherregistry.tld":{"username":"user","password":"swordfish"},`,
		`"https://index.docker.io/v1/":{"username":"dockerhub","password":"password"}`,
		`}`), ""))

	providerCfg := NewDockerAuthProvider("dockercfg", authData)
	providerDocker := NewDockerAuthProvider("docker", dockerAuthData)

	for ndx, pair := range expectedPairs {
		authConfig, _ := providerCfg.GetAuthconfig(pair.Image, nil)
		if authConfig.Username != pair.ExpectedUser || authConfig.Password != pair.ExpectedPass {
			t.Errorf("Expectation failure: #%v. Got %v, wanted %v", ndx, authConfig, pair)
		}

		authConfig, _ = providerDocker.GetAuthconfig(pair.Image, nil)
		if authConfig.Username != pair.ExpectedUser || authConfig.Password != pair.ExpectedPass {
			t.Errorf("Expectation failure: #%v. Got %v, wanted %v", ndx, authConfig, pair)
		}
	}
}

func TestAuthAppliesToOnlyRegistry(t *testing.T) {
	authData := []byte(`{
		"example.tld/user2":{"auth":"` + secretAuth + `","email":"test@test.test"}
	}`)

	var expectedPairs = []authTestPair{
		// '/user2' should apply here because of matching hostname
		{"example.tld/foo", "user", "swordfish"},
		{"registry.tld", "", ""},
		{"nginx", "", ""},
	}

	provider := NewDockerAuthProvider("dockercfg", authData)

	for ndx, pair := range expectedPairs {
		authConfig, _ := provider.GetAuthconfig(pair.Image, nil)
		if authConfig.Username != pair.ExpectedUser || authConfig.Password != pair.ExpectedPass {
			t.Errorf("Expectation failure: #%v. Got %v, wanted %v", ndx, authConfig, pair)
		}
	}
}

func TestAuthErrors(t *testing.T) {
	badPairs := []struct {
		t string
		a string
	}{
		{"docker", `{"registry.tld":{"auth":"` + secretAuth + `"}}`},
		{"docker", `{"registry.tld":{"username":true}}`},
		{"dockercfg", `{"registry.tld":{"auth":"malformedbase64"}}`},
		{"dockercfg", `{"registry.tld":{"auth":true}}`},
		{"dockercfg", `{"registry.tld":{"auth":"` + base64.StdEncoding.EncodeToString([]byte("noColon")) + `"}}`},
		{"invalid", ""},
	}

	for _, pair := range badPairs {
		provider := NewDockerAuthProvider(pair.t, []byte(pair.a))
		result, _ := provider.GetAuthconfig("nginx", nil)
		if !reflect.DeepEqual(result, docker.AuthConfiguration{}) {
			t.Errorf("Expected empty auth config for %v; got %v", pair, result)
		}
	}

}

func TestEmptyConfig(t *testing.T) {
	provider := NewDockerAuthProvider("", []byte(""))
	authConfig, _ := provider.GetAuthconfig("nginx", nil)
	if !reflect.DeepEqual(authConfig, docker.AuthConfiguration{}) {
		t.Errorf("Expected empty authconfig to not return any auth data at all")
	}
}
