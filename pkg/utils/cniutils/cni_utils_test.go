package cniutils

import (
	"net"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/stretchr/testify/assert"
)

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
