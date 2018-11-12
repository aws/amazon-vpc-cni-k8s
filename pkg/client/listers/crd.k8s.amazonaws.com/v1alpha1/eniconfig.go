// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd.k8s.amazonaws.com/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ENIConfigLister helps list ENIConfigs.
type ENIConfigLister interface {
	// List lists all ENIConfigs in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.ENIConfig, err error)
	// Get retrieves the ENIConfig from the index for a given name.
	Get(name string) (*v1alpha1.ENIConfig, error)
	ENIConfigListerExpansion
}

// eNIConfigLister implements the ENIConfigLister interface.
type eNIConfigLister struct {
	indexer cache.Indexer
}

// NewENIConfigLister returns a new ENIConfigLister.
func NewENIConfigLister(indexer cache.Indexer) ENIConfigLister {
	return &eNIConfigLister{indexer: indexer}
}

// List lists all ENIConfigs in the indexer.
func (s *eNIConfigLister) List(selector labels.Selector) (ret []*v1alpha1.ENIConfig, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ENIConfig))
	})
	return ret, err
}

// Get retrieves the ENIConfig from the index for a given name.
func (s *eNIConfigLister) Get(name string) (*v1alpha1.ENIConfig, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("eniconfig"), name)
	}
	return obj.(*v1alpha1.ENIConfig), nil
}
