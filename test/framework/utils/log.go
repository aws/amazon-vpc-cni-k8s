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
	"github.com/go-logr/logr"
	ginkgov2 "github.com/onsi/ginkgo/v2"
	zapraw "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// NewGinkgoLogger returns new logger with ginkgo backend.
func NewGinkgoLogger() logr.Logger {
	encoder := zapcore.NewJSONEncoder(zapraw.NewProductionEncoderConfig())
	return zap.New(zap.UseDevMode(false),
		zap.Level(zapraw.InfoLevel),
		zap.WriteTo(ginkgov2.GinkgoWriter),
		zap.Encoder(encoder))
}
