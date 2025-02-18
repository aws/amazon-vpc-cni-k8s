package ipamd

import (
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

var primaryInstance types.Instance
var f *framework.Framework
var err error

func ceil(x, y int) int {
	return (x + y - 1) / y
}

func Max(x, y int) int {
	if x < y {
		return y
	}
	return x
}

// MinIgnoreZero returns smaller of two number, if any number is zero returns the other number
func MinIgnoreZero(x, y int) int {
	if x == 0 {
		return y
	}
	if y == 0 {
		return x
	}
	if x < y {
		return x
	}
	return y
}
