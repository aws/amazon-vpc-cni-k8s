package networkutils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGeneratePodHostVethName(t *testing.T) {
	type args struct {
		prefix       string
		podNamespace string
		podName      string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "with eni as prefix",
			args: args{
				prefix:       "eni",
				podNamespace: "kube-system",
				podName:      "coredns-57ff979f67-qqbdh",
			},
			want: "enib5faff8a083",
		},
		{
			name: "with vlan as prefix",
			args: args{
				prefix:       "vlan",
				podNamespace: "kube-system",
				podName:      "coredns-57ff979f67-qqbdh",
			},
			want: "vlanb5faff8a083",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GeneratePodHostVethName(tt.args.prefix, tt.args.podNamespace, tt.args.podName, 0)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGeneratePodHostVethNameSuffix(t *testing.T) {
	type args struct {
		podNamespace string
		podName      string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "kube-system/coredns-57ff979f67-qqbdh",
			args: args{
				podNamespace: "kube-system",
				podName:      "coredns-57ff979f67-qqbdh",
			},
			want: "b5faff8a083",
		},
		{
			name: "kube-system/coredns-57ff979f67-8ns9b",
			args: args{
				podNamespace: "kube-system",
				podName:      "coredns-57ff979f67-8ns9b",
			},
			want: "9571956a6cc",
		},
		{
			name: "default/sample-pod",
			args: args{
				podNamespace: "default",
				podName:      "sample-pod",
			},
			want: "cc21c2d7785",
		},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := GeneratePodHostVethNameSuffix(tt.args.podNamespace, tt.args.podName)
			assert.Equal(t, tt.want, got)
		})
	}
}
