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
		{"minimum spanning capacity", 2, 3, 4},
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
			got, err := SpanningENIsReplicaCount(netInfo)
			if err != nil {
				t.Fatalf("SpanningENIsReplicaCount(%d ENIs, %d IPs) returned error: %v",
					tc.maxENIs, tc.ipsPerENI, err)
			}
			if got != tc.want {
				t.Errorf("SpanningENIsReplicaCount(%d ENIs, %d IPs) = %d, want %d",
					tc.maxENIs, tc.ipsPerENI, got, tc.want)
			}

			// The count must exceed total secondary-ENI capacity so at least one pod
			// is forced onto the primary ENI, and must stay within the secondary-IP
			// max-pods ceiling so the node can schedule it.
			secondaryCapacity := (int(tc.maxENIs) - 1) * (int(tc.ipsPerENI) - 1)
			// replicas = max-pods - (ipsPerENI-1) by construction, so a single
			// spanning deployment always fits the node scheduling cap; this is why
			// AssertSpanningENIsPreconditions has no capacity check.
			maxPods := int(tc.maxENIs)*(int(tc.ipsPerENI)-1) + 2
			if got <= secondaryCapacity {
				t.Errorf("replica count %d does not exceed secondary capacity %d; pods may all fit on secondary ENIs",
					got, secondaryCapacity)
			}
			if got != maxPods-(int(tc.ipsPerENI)-1) {
				t.Errorf("replica count %d != max-pods %d minus primary-ENI capacity %d; the fits-by-construction algebra no longer holds",
					got, maxPods, int(tc.ipsPerENI)-1)
			}
		})
	}
}

func TestSpanningENIsReplicaCountRejectsInvalidNetworkInfo(t *testing.T) {
	i32 := func(v int32) *int32 { return &v }

	cases := []struct {
		name    string
		netInfo ec2types.NetworkInfo
	}{
		{"missing maximum ENIs", ec2types.NetworkInfo{Ipv4AddressesPerInterface: i32(10)}},
		{"missing IPs per ENI", ec2types.NetworkInfo{MaximumNetworkInterfaces: i32(3)}},
		{"single ENI", ec2types.NetworkInfo{MaximumNetworkInterfaces: i32(1), Ipv4AddressesPerInterface: i32(10)}},
		{"no secondary IPs", ec2types.NetworkInfo{MaximumNetworkInterfaces: i32(3), Ipv4AddressesPerInterface: i32(1)}},
		{"only one pod IP per ENI", ec2types.NetworkInfo{MaximumNetworkInterfaces: i32(3), Ipv4AddressesPerInterface: i32(2)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SpanningENIsReplicaCount(tc.netInfo); err == nil {
				t.Fatal("SpanningENIsReplicaCount() returned no error")
			}
		})
	}
}

func TestParseBoolEnv(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"t", true},
		{"false", false},
		{"0", false},
		{"", false},
		{"invalid", false},
	}

	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			if got := parseBoolEnv(tc.value); got != tc.want {
				t.Errorf("parseBoolEnv(%q) = %t, want %t", tc.value, got, tc.want)
			}
		})
	}
}
