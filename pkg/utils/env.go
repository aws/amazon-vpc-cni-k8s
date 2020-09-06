package utils

import (
	"fmt"
	"os"
	"strconv"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

// GetBoolEnvVar is a helper function to read a boolean environment variable
func GetBoolEnvVar(log logger.Logger, name string, defaultValue bool) bool {
	if strValue := os.Getenv(name); strValue != "" {
		parsedValue, err := strconv.ParseBool(strValue)
		if err == nil {
			return parsedValue
		}
		log.Errorf("Failed to parse "+name+"; using default: "+fmt.Sprint(defaultValue), err.Error())
	}
	return defaultValue
}
