package cniutils

import (
	"errors"
	"fmt"
	"net"
	"testing"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/aws/aws-sdk-go-v2/aws"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/stretchr/testify/assert"
)

type testNotExistError bool

func (e testNotExistError) Error() string {
	return "iptables error"
}

func (e testNotExistError) IsNotExist() bool {
	return bool(e)
}

func TestIsChainNotExistErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil"},
		{name: "typed not found", err: testNotExistError(true), want: true},
		{name: "wrapped typed not found", err: fmt.Errorf("wrapped: %w", testNotExistError(true)), want: true},
		{name: "typed error is not not-found", err: testNotExistError(false)},
		{name: "no chain message", err: errors.New("iptables: No chain/target/match by that name."), want: true},
		{name: "does not exist message", err: errors.New("rule does not exist"), want: true},
		{name: "ordinary error", err: errors.New("permission denied")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsChainNotExistErr(tt.err))
		})
	}
}

func TestIsIptableTargetNotExist(t *testing.T) {
	assert.True(t, IsIptableTargetNotExist(testNotExistError(true)))
	assert.False(t, IsIptableTargetNotExist(nil))
}

func Test_FindInterfaceByName(t *testing.T) {
	type args struct {
		ifaceList []*current.Interface
		ifaceName string
	}
	tests := []struct {
		name           string
		args           args
		wantIfaceIndex int
		wantIface      *current.Interface
		wantFound      bool
	}{
		{
			name: "found the CNI interface at index 0",
			args: args{
				ifaceList: []*current.Interface{
					{
						Name: "eni8ea2c11fe35",
					},
					{
						Name: "eth0",
					},
				},
				ifaceName: "eni8ea2c11fe35",
			},
			wantIfaceIndex: 0,
			wantIface: &current.Interface{
				Name: "eni8ea2c11fe35",
			},
			wantFound: true,
		},
		{
			name: "found the CNI interface at index 1",
			args: args{
				ifaceList: []*current.Interface{
					{
						Name: "eth0",
					},
					{
						Name: "eni8ea2c11fe35",
					},
				},
				ifaceName: "eni8ea2c11fe35",
			},
			wantIfaceIndex: 1,
			wantIface: &current.Interface{
				Name: "eni8ea2c11fe35",
			},
			wantFound: true,
		},
		{
			name: "didn't found CNI interface",
			args: args{
				ifaceList: []*current.Interface{
					{
						Name: "eth0",
					},
					{
						Name: "eni8ea2c11fe35",
					},
				},
				ifaceName: "enixxxxx",
			},
			wantFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIfaceIndex, gotIface, gotFound := FindInterfaceByName(tt.args.ifaceList, tt.args.ifaceName)
			assert.Equal(t, tt.wantFound, gotFound)
			if tt.wantFound {
				assert.Equal(t, tt.wantIfaceIndex, gotIfaceIndex)
				assert.Equal(t, tt.wantIface, gotIface)
			}
		})
	}
}

func Test_FindIPConfigsByIfaceIndex(t *testing.T) {
	type args struct {
		ipConfigs  []*current.IPConfig
		ifaceIndex int
	}
	tests := []struct {
		name string
		args args
		want []*current.IPConfig
	}{
		{
			name: "single matched IPConfig",
			args: args{
				ipConfigs: []*current.IPConfig{
					{
						Interface: aws.Int(1),
						Address: net.IPNet{
							IP: net.ParseIP("192.168.1.1"),
						},
					},
					{
						Interface: aws.Int(2),
						Address: net.IPNet{
							IP: net.ParseIP("192.168.1.2"),
						},
					},
				},
				ifaceIndex: 1,
			},
			want: []*current.IPConfig{
				{
					Interface: aws.Int(1),
					Address: net.IPNet{
						IP: net.ParseIP("192.168.1.1"),
					},
				},
			},
		},
		{
			name: "multiple matched IPConfig",
			args: args{
				ipConfigs: []*current.IPConfig{
					{
						Interface: aws.Int(1),
						Address: net.IPNet{
							IP: net.ParseIP("192.168.1.1"),
						},
					},
					{
						Interface: aws.Int(1),
						Address: net.IPNet{
							IP: net.ParseIP("192.168.1.2"),
						},
					},
					{
						Interface: aws.Int(2),
						Address: net.IPNet{
							IP: net.ParseIP("192.168.1.3"),
						},
					},
				},
				ifaceIndex: 1,
			},
			want: []*current.IPConfig{
				{
					Interface: aws.Int(1),
					Address: net.IPNet{
						IP: net.ParseIP("192.168.1.1"),
					},
				},
				{
					Interface: aws.Int(1),
					Address: net.IPNet{
						IP: net.ParseIP("192.168.1.2"),
					},
				},
			},
		},
		{
			name: "none matched IPConfig",
			args: args{
				ipConfigs: []*current.IPConfig{
					{
						Interface: aws.Int(2),
						Address: net.IPNet{
							IP: net.ParseIP("192.168.1.1"),
						},
					},
					{
						Interface: aws.Int(2),
						Address: net.IPNet{
							IP: net.ParseIP("192.168.1.2"),
						},
					},
				},
				ifaceIndex: 1,
			},
			want: nil,
		},
		{
			name: "interface is not set",
			args: args{
				ipConfigs: []*current.IPConfig{
					{
						Address: net.IPNet{
							IP: net.ParseIP("192.168.1.1"),
						},
					},
				},
				ifaceIndex: 1,
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindIPConfigsByIfaceIndex(tt.args.ipConfigs, tt.args.ifaceIndex)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPrefixSimilar(t *testing.T) {
	tests := []struct {
		name        string
		prefixPool  []string
		eniPrefixes []ec2types.Ipv4PrefixSpecification
		want        bool
	}{
		{
			name:        "Empty slices",
			prefixPool:  []string{},
			eniPrefixes: []ec2types.Ipv4PrefixSpecification{},
			want:        true,
		},
		{
			name:        "Different lengths",
			prefixPool:  []string{"192.168.1.0/24"},
			eniPrefixes: []ec2types.Ipv4PrefixSpecification{},
			want:        false,
		},
		{
			name:       "Equivalent prefixes",
			prefixPool: []string{"192.168.1.0/24", "10.0.0.0/16"},
			eniPrefixes: []ec2types.Ipv4PrefixSpecification{
				{Ipv4Prefix: stringPtr("192.168.1.0/24")},
				{Ipv4Prefix: stringPtr("10.0.0.0/16")},
			},
			want: true,
		},
		{
			name:       "Different prefixes",
			prefixPool: []string{"192.168.1.0/24", "10.0.0.0/16"},
			eniPrefixes: []ec2types.Ipv4PrefixSpecification{
				{Ipv4Prefix: stringPtr("192.168.1.0/24")},
				{Ipv4Prefix: stringPtr("172.16.0.0/16")},
			},
			want: false,
		},
		{
			name:       "Nil prefix",
			prefixPool: []string{"192.168.1.0/24"},
			eniPrefixes: []ec2types.Ipv4PrefixSpecification{
				{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PrefixSimilar(tt.prefixPool, tt.eniPrefixes); got != tt.want {
				t.Errorf("in test %s PrefixSimilar() = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIPsSimilar(t *testing.T) {
	tests := []struct {
		name   string
		ipPool []string
		eniIPs []ec2types.NetworkInterfacePrivateIpAddress
		want   bool
	}{
		{
			name:   "Empty IP pool",
			ipPool: []string{},
			eniIPs: []ec2types.NetworkInterfacePrivateIpAddress{
				{PrivateIpAddress: stringPtr("10.0.0.1"), Primary: boolPtr(true)},
			},
			want: true,
		},
		{
			name:   "Different lengths",
			ipPool: []string{"192.168.1.1"},
			eniIPs: []ec2types.NetworkInterfacePrivateIpAddress{
				{PrivateIpAddress: stringPtr("10.0.0.1"), Primary: boolPtr(true)},
				{PrivateIpAddress: stringPtr("192.168.1.1"), Primary: boolPtr(false)},
				{PrivateIpAddress: stringPtr("192.168.1.2"), Primary: boolPtr(false)},
			},
			want: false,
		},
		{
			name:   "Equivalent IPs",
			ipPool: []string{"192.168.1.1", "10.0.0.2"},
			eniIPs: []ec2types.NetworkInterfacePrivateIpAddress{
				{PrivateIpAddress: stringPtr("10.0.0.1"), Primary: boolPtr(true)},
				{PrivateIpAddress: stringPtr("192.168.1.1"), Primary: boolPtr(false)},
				{PrivateIpAddress: stringPtr("10.0.0.2"), Primary: boolPtr(false)},
			},
			want: true,
		},
		{
			name:   "Different IPs",
			ipPool: []string{"192.168.1.1", "10.0.0.2"},
			eniIPs: []ec2types.NetworkInterfacePrivateIpAddress{
				{PrivateIpAddress: stringPtr("10.0.0.1"), Primary: boolPtr(true)},
				{PrivateIpAddress: stringPtr("192.168.1.1"), Primary: boolPtr(false)},
				{PrivateIpAddress: stringPtr("172.16.0.1"), Primary: boolPtr(false)},
			},
			want: false,
		},
		{
			name:   "Nil IP",
			ipPool: []string{"192.168.1.1"},
			eniIPs: []ec2types.NetworkInterfacePrivateIpAddress{
				{PrivateIpAddress: stringPtr("10.0.0.1"), Primary: boolPtr(true)},
				{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IPsSimilar(tt.ipPool, tt.eniIPs); got != tt.want {
				t.Errorf("in test %s IPsSimilar() = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// Helper functions for creating pointers
func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}
