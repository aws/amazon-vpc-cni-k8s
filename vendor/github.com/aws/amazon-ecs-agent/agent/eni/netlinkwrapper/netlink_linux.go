// +build linux

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

package netlinkwrapper

import "github.com/vishvananda/netlink"

// NetLink Wrapper methods used from the vishvananda/netlink package
type NetLink interface {
	LinkByName(name string) (netlink.Link, error)
	LinkList() ([]netlink.Link, error)
}

// NetLinkClient helps invoke the actual netlink methods
type NetLinkClient struct{}

// New creates a new NetLink object
func New() NetLink {
	return NetLinkClient{}
}

// LinkByName finds a link by name and returns a pointer to the object
func (NetLinkClient) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}

// LinkList gets a list of link devices. Equivalent to: `ip link show`
func (NetLinkClient) LinkList() ([]netlink.Link, error) {
	return netlink.LinkList()
}
