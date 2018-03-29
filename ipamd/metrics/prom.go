// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package metrics

import (
	"sync"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	mregL sync.RWMutex
	mreg  bool
)

func (m *Metrics) Register() error {
	mregL.RLock()
	already := mreg
	mregL.RUnlock()
	if already {
		return nil
	}

	if err := prometheus.Register(m.awsLatency); err != nil {
		return errors.Wrap(err, "registering awsLatency")
	}
	if err := prometheus.Register(m.addEniRetries); err != nil {
		return errors.Wrap(err, "registering addEniRetries")
	}
	if err := prometheus.Register(m.ipCount); err != nil {
		return errors.Wrap(err, "registering ipCount")
	}
	if err := prometheus.Register(m.eniCount); err != nil {
		return errors.Wrap(err, "registering eniCount")
	}

	return nil
}
