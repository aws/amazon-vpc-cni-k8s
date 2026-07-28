package common

import (
	"testing"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestSpanningENIsReplicaCount(t *testing.T) {
	i32 := func(v int32) *int32 { return &v }

	cases := []struct {
		name      string
		maxENIs   int32
		ipsPerENI int32
		want      int
	}{
		{"c5.large", 3, 10, 20},
		{"c5.xlarge", 4, 15, 44},
		{"t3.medium", 3, 6, 12},
		{"c5.18xlarge", 15, 50, 688},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			netInfo := ec2types.NetworkInfo{
				MaximumNetworkInterfaces:  i32(tc.maxENIs),
				Ipv4AddressesPerInterface: i32(tc.ipsPerENI),
			}
			got := SpanningENIsReplicaCount(netInfo)
			if got != tc.want {
				t.Errorf("SpanningENIsReplicaCount(%d ENIs, %d IPs) = %d, want %d",
					tc.maxENIs, tc.ipsPerENI, got, tc.want)
			}

			// The count must exceed total secondary-ENI capacity so at least one pod
			// is forced onto the primary ENI, and must stay within the secondary-IP
			// max-pods ceiling so the node can schedule it.
			secondaryCapacity := (int(tc.maxENIs) - 1) * (int(tc.ipsPerENI) - 1)
			maxPods := int(tc.maxENIs)*(int(tc.ipsPerENI)-1) + 2
			if got <= secondaryCapacity {
				t.Errorf("replica count %d does not exceed secondary capacity %d; pods may all fit on secondary ENIs",
					got, secondaryCapacity)
			}
			if got > maxPods {
				t.Errorf("replica count %d exceeds max-pods ceiling %d", got, maxPods)
			}
		})
	}
}
