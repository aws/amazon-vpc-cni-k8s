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
