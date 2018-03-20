package eni

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type mockEC2 struct {
	mDescribeInstancesWithContext func(aws.Context, *ec2.DescribeInstancesInput, ...request.Option) (*ec2.DescribeInstancesOutput, error)
}

func (m *mockEC2) DescribeInstancesWithContext(c aws.Context, i *ec2.DescribeInstancesInput, o ...request.Option) (*ec2.DescribeInstancesOutput, error) {
	return m.mDescribeInstancesWithContext(c, i, o...)
}
func (*mockEC2) CreateNetworkInterfaceWithContext(aws.Context, *ec2.CreateNetworkInterfaceInput, ...request.Option) (*ec2.CreateNetworkInterfaceOutput, error) {
	return nil, nil
}
func (*mockEC2) AttachNetworkInterfaceWithContext(aws.Context, *ec2.AttachNetworkInterfaceInput, ...request.Option) (*ec2.AttachNetworkInterfaceOutput, error) {
	return nil, nil
}
func (*mockEC2) ModifyNetworkInterfaceAttributeWithContext(aws.Context, *ec2.ModifyNetworkInterfaceAttributeInput, ...request.Option) (*ec2.ModifyNetworkInterfaceAttributeOutput, error) {
	return nil, nil
}
func (*mockEC2) DetachNetworkInterfaceWithContext(aws.Context, *ec2.DetachNetworkInterfaceInput, ...request.Option) (*ec2.DetachNetworkInterfaceOutput, error) {
	return nil, nil
}
func (*mockEC2) DeleteNetworkInterfaceWithContext(aws.Context, *ec2.DeleteNetworkInterfaceInput, ...request.Option) (*ec2.DeleteNetworkInterfaceOutput, error) {
	return nil, nil
}
