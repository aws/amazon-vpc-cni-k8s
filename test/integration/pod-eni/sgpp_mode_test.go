package pod_eni

import (
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"
)

func TestExpectTCPEarlyDemuxRestart(t *testing.T) {
	tests := []struct {
		name                 string
		mode                 sgpp.EnforcingMode
		disableTCPEarlyDemux string
		want                 bool
	}{
		{
			name:                 "strict mode with early demux enabled",
			mode:                 sgpp.EnforcingModeStrict,
			disableTCPEarlyDemux: "false",
			want:                 true,
		},
		{
			name:                 "strict mode with early demux disabled",
			mode:                 sgpp.EnforcingModeStrict,
			disableTCPEarlyDemux: "true",
			want:                 false,
		},
		{
			name:                 "standard mode with early demux enabled",
			mode:                 sgpp.EnforcingModeStandard,
			disableTCPEarlyDemux: "false",
			want:                 false,
		},
		{
			name:                 "standard mode with early demux disabled",
			mode:                 sgpp.EnforcingModeStandard,
			disableTCPEarlyDemux: "true",
			want:                 false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expectTCPEarlyDemuxRestart(tt.mode, tt.disableTCPEarlyDemux); got != tt.want {
				t.Errorf("expectTCPEarlyDemuxRestart(%q, %q) = %t, want %t",
					tt.mode, tt.disableTCPEarlyDemux, got, tt.want)
			}
		})
	}
}
