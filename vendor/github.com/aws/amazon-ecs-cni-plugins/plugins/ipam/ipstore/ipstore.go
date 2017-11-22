// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package ipstore

import (
	"math/big"
	"net"
	"time"

	"github.com/docker/libkv/store"
	"github.com/docker/libkv/store/boltdb"
	"github.com/pkg/errors"
)

const (
	// Mask size beyond 30 won't be accepted since the .0 and .255 are reserved
	MaxMask = 30
	// This assumes all the ip start with 1 for now before we address
	// the issue: https://github.com/aws/amazon-ecs-cni-plugins/issues/37
	IPPrefix = "1"
)

// IPManager is responsible for managing the ip addresses using boltdb
type IPManager struct {
	client      store.Store
	subnet      net.IPNet
	lastKnownIP net.IP
}

// Config represents the configuration for boltdb opermation where the
// ip address are stored
type Config struct {
	DB                string
	PersistConnection bool
	Bucket            string
	ConnectionTimeout time.Duration
}

// IPAllocator defines the operation for an ip allocator
type IPAllocator interface {
	GetAvailableIP(string) (string, error)
	Get(string) (string, error)
	Assign(string, string) error
	Update(string, string) error
	Release(string) error
	ReleaseByID(string) (string, error)
	Exists(string) (bool, error)
	SetLastKnownIP(net.IP)
	Close()
}

// NewIPAllocator creates an ip manager from the IPAM and  db configuration
func NewIPAllocator(options *Config, subnet net.IPNet) (IPAllocator, error) {
	config := &store.Config{
		PersistConnection: options.PersistConnection,
		ConnectionTimeout: options.ConnectionTimeout,
		Bucket:            options.Bucket,
	}
	client, err := boltdb.New([]string{options.DB}, config)
	if err != nil {
		return nil, err
	}

	return &IPManager{
		client:      client,
		subnet:      subnet,
		lastKnownIP: subnet.IP.Mask(subnet.Mask),
	}, nil
}

// SetLastKnownIP updates the record of last visited ip address
func (manager *IPManager) SetLastKnownIP(ip net.IP) {
	manager.lastKnownIP = ip
}

// GetAvailableIP returns the next available ip address
func (manager *IPManager) GetAvailableIP(id string) (string, error) {
	var err error
	nextIP := manager.lastKnownIP
	for {
		nextIP, err = NextIP(nextIP, manager.subnet)
		if err != nil {
			return "", err
		}
		err = manager.Assign(nextIP.String(), id)
		if err != nil && err != store.ErrKeyExists {
			return "", errors.Wrapf(err, "ipstore get ip: failed to assign ip in ipam")
		} else if err == store.ErrKeyExists {
			// ip already be used
		} else {
			// assing the ip succeed
			manager.lastKnownIP = nextIP
			return nextIP.String(), nil
		}
		if nextIP.Equal(manager.lastKnownIP) {
			return "", errors.New("getAvailableIP ipstore: failed to find available ip addresses in the subnet")
		}
	}
}

// Get returns the id by which the ip was used and return empty if the key not exists
func (manager *IPManager) Get(ip string) (string, error) {
	kvPair, err := manager.client.Get(ip)
	if err != nil && err != store.ErrKeyNotFound {
		return "", errors.Wrapf(err, "get ipstore: failed to get %v from db", kvPair)
	}
	if err == store.ErrKeyNotFound {
		return "", nil
	}

	return string(kvPair.Value), nil
}

// Assign marks the ip as used or return an error if the ip has already been used
func (manager *IPManager) Assign(ip string, id string) error {
	ok, err := manager.Exists(ip)
	if err != nil {
		return errors.Wrapf(err, "assign ipstore: query the db failed, err: %v", err)
	}
	if ok {
		return store.ErrKeyExists
	}

	// if the id presents, it should be unique
	if id != "" {
		ok, err := manager.UniqueID(id)
		if err != nil {
			return errors.Wrapf(err, "assign ipstore: check id unique failed, id: %s", id)
		}
		if !ok {
			return errors.Errorf("assign ipstore: id already exists, id: %s", id)
		}
	}

	err = manager.client.Put(ip, []byte(id), nil)
	if err != nil {
		return errors.Wrapf(err, "assign ipstore: failed to put the key/value into the db: %s -> %s", ip, id)
	}

	manager.lastKnownIP = net.ParseIP(ip)
	return nil
}

// Release marks the ip as available or return an error if
// the ip is avaialble already
func (manager *IPManager) Release(ip string) error {
	ok, err := manager.Exists(ip)
	if err != nil {
		return errors.Wrap(err, "release ipstore: failed to query the db")
	}
	if !ok {
		return errors.Errorf("release ipstore: ip does not existed in the db: %s", ip)
	}

	err = manager.client.Delete(ip)
	if err != nil {
		return errors.Wrap(err, "release ipstore: failed to delete the key in the db")
	}

	manager.lastKnownIP = net.ParseIP(ip)

	return nil
}

// ReleaseByID release the key-value pair by id
func (manager *IPManager) ReleaseByID(id string) (string, error) {
	// TODO improve this part by implement listing all the kv pairs
	// libkv library only provide list all key-value pairs with prefix of key, and
	// if the result is empty it will return store.ErrKeyNotFound error
	kvPairs, err := manager.client.List(IPPrefix)
	if err == store.ErrKeyNotFound {
		return "", errors.Errorf("release ipstore: no ip associated with the id: %s", id)
	}
	if err != nil {
		return "", errors.Wrapf(err, "release ipstore: failed to list the key-value pairs in db")
	}

	var ipDeleted string
	for _, kvPair := range kvPairs {
		if string(kvPair.Value) == id {
			err = manager.Release(kvPair.Key)
			if err != nil {
				return "", err
			}
			ipDeleted = kvPair.Key
			return ipDeleted, nil
		}
	}
	return "", errors.Errorf("release ipstore: no ip address associated with the given id: %s", id)
}

// UniqueID checks whether the id has already existed in the ipam
func (manager *IPManager) UniqueID(id string) (bool, error) {
	kvPairs, err := manager.client.List(IPPrefix)
	// TODO improve this part by implement listing all the kv pairs
	if err == store.ErrKeyNotFound {
		return true, nil
	}

	if err != nil {
		return false, errors.Wrapf(err, "ipstore: failed to list the key-value pairs in db")
	}

	for _, kvPair := range kvPairs {
		if string(kvPair.Value) == id {
			return false, errors.Errorf("ipstore: id already exists")
		}
	}
	return true, nil
}

// Update updates the value of existed key in the db
func (manager *IPManager) Update(key string, value string) error {
	return manager.client.Put(key, []byte(value), nil)
}

// Exists checks whether the ip is used or not
func (manager *IPManager) Exists(ip string) (bool, error) {
	ok, err := manager.client.Exists(ip)
	if err == store.ErrKeyNotFound {
		return false, nil
	}

	return ok, err
}

// Close will close the connection to the db
func (manager *IPManager) Close() {
	manager.client.Close()
}

// NextIP returns the next ip in the subnet
func NextIP(ip net.IP, subnet net.IPNet) (net.IP, error) {
	if ones, _ := subnet.Mask.Size(); ones > MaxMask {
		return nil, errors.Errorf("nextIP ipstore: no available ip in the subnet: %v", subnet)
	}

	// currently only ipv4 is supported
	ipv4 := ip.To4()
	if ipv4 == nil {
		return nil, errors.Errorf("nextIP ipstore: invalid ipv4 address: %v", ipv4)
	}

	if !subnet.Contains(ipv4) {
		return nil, errors.Errorf("nextIP ipstore: ip %v is not within subnet %s", ipv4.String(), subnet.String())
	}

	minIP := subnet.IP.Mask(subnet.Mask)
	maxIP := net.IP(make([]byte, 4))
	for i := range ipv4 {
		maxIP[i] = minIP[i] | ^subnet.Mask[i]
	}

	nextIP := ipv4
	// Reserve the broadcast address(all 1) and the network address(all 0)
	for nextIP.Equal(ipv4) || nextIP.Equal(minIP) || nextIP.Equal(maxIP) {
		if nextIP.Equal(maxIP) {
			nextIP = minIP
		}
		// convert the IP into Int for easily calculation
		nextIPInBytes := big.NewInt(0).SetBytes(nextIP)
		nextIPInBytes.Add(nextIPInBytes, big.NewInt(1))
		nextIP = net.IP(nextIPInBytes.Bytes())
	}

	return nextIP, nil
}
