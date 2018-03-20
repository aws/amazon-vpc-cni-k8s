// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package eni

// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI
type l struct {
	ENILimit  int64
	IPv4Limit int64
}

// TODO(tvi): Estimate reasonable value.
var defaultLimit = l{5, 15}

var eniLimit = map[string]l{
	"c1.medium":    l{2, 6},
	"c1.xlarge":    l{4, 15},
	"c3.large":     l{3, 10},
	"c3.xlarge":    l{4, 15},
	"c3.2xlarge":   l{4, 15},
	"c3.4xlarge":   l{8, 30},
	"c3.8xlarge":   l{8, 30},
	"c4.large":     l{3, 10},
	"c4.xlarge":    l{4, 15},
	"c4.2xlarge":   l{4, 15},
	"c4.4xlarge":   l{8, 30},
	"c4.8xlarge":   l{8, 30},
	"c5.large":     l{3, 10},
	"c5.xlarge":    l{4, 15},
	"c5.2xlarge":   l{4, 15},
	"c5.4xlarge":   l{8, 30},
	"c5.9xlarge":   l{8, 30},
	"c5.18xlarge":  l{15, 50},
	"cc2.8xlarge":  l{8, 30},
	"cr1.8xlarge":  l{8, 30},
	"d2.xlarge":    l{4, 15},
	"d2.2xlarge":   l{4, 15},
	"d2.4xlarge":   l{8, 30},
	"d2.8xlarge":   l{8, 30},
	"f1.2xlarge":   l{4, 15},
	"f1.16xlarge":  l{8, 50},
	"g2.2xlarge":   l{4, 15},
	"g2.8xlarge":   l{8, 30},
	"g3.4xlarge":   l{8, 30},
	"g3.8xlarge":   l{8, 30},
	"g3.16xlarge":  l{15, 50},
	"h1.2xlarge":   l{4, 15},
	"h1.4xlarge":   l{8, 30},
	"h1.8xlarge":   l{8, 30},
	"h1.16xlarge":  l{15, 50},
	"hs1.8xlarge":  l{8, 30},
	"i2.xlarge":    l{4, 15},
	"i2.2xlarge":   l{4, 15},
	"i2.4xlarge":   l{8, 30},
	"i2.8xlarge":   l{8, 30},
	"i3.large":     l{3, 10},
	"i3.xlarge":    l{4, 15},
	"i3.2xlarge":   l{4, 15},
	"i3.4xlarge":   l{8, 30},
	"i3.8xlarge":   l{8, 30},
	"i3.16xlarge":  l{15, 50},
	"m1.small":     l{2, 4},
	"m1.medium":    l{2, 6},
	"m1.large":     l{3, 10},
	"m1.xlarge":    l{4, 15},
	"m2.xlarge":    l{4, 15},
	"m2.2xlarge":   l{4, 30},
	"m2.4xlarge":   l{8, 30},
	"m3.medium":    l{2, 6},
	"m3.large":     l{3, 10},
	"m3.xlarge":    l{4, 15},
	"m3.2xlarge":   l{4, 30},
	"m4.large":     l{2, 10},
	"m4.xlarge":    l{4, 15},
	"m4.2xlarge":   l{4, 15},
	"m4.4xlarge":   l{8, 30},
	"m4.10xlarge":  l{8, 30},
	"m4.16xlarge":  l{8, 30},
	"m5.large":     l{3, 10},
	"m5.xlarge":    l{4, 15},
	"m5.2xlarge":   l{4, 15},
	"m5.4xlarge":   l{8, 30},
	"m5.12xlarge":  l{8, 30},
	"m5.24xlarge":  l{15, 50},
	"p2.xlarge":    l{4, 15},
	"p2.8xlarge":   l{8, 30},
	"p2.16xlarge":  l{8, 30},
	"p3.2xlarge":   l{4, 15},
	"p3.8xlarge":   l{8, 30},
	"p3.16xlarge":  l{8, 30},
	"r3.large":     l{3, 10},
	"r3.xlarge":    l{4, 15},
	"r3.2xlarge":   l{4, 15},
	"r3.4xlarge":   l{8, 30},
	"r3.8xlarge":   l{8, 30},
	"r4.large":     l{3, 10},
	"r4.xlarge":    l{4, 15},
	"r4.2xlarge":   l{4, 15},
	"r4.4xlarge":   l{8, 30},
	"r4.8xlarge":   l{8, 30},
	"r4.16xlarge":  l{15, 50},
	"t1.micro":     l{2, 2},
	"t2.nano":      l{2, 2},
	"t2.micro":     l{2, 2},
	"t2.small":     l{2, 4},
	"t2.medium":    l{3, 6},
	"t2.large":     l{3, 12},
	"t2.xlarge":    l{3, 15},
	"t2.2xlarge":   l{3, 15},
	"x1.16xlarge":  l{8, 30},
	"x1.32xlarge":  l{8, 30},
	"x1e.xlarge":   l{3, 10},
	"x1e.2xlarge":  l{4, 15},
	"x1e.4xlarge":  l{4, 15},
	"x1e.8xlarge":  l{4, 15},
	"x1e.16xlarge": l{8, 30},
	"x1e.32xlarge": l{8, 30},
}
