# ECS IPAM plugin

## Overview

The ECS IPAM plugin constructs the IP, Gateway and Routes, which are used 
by the ECS Bridge plugin to configure the bridge and veth pair in the 
container network namespace. An example of this configuration looks like:
```json
{
    "ipam": {
        "type": "ecs-ipam",
        "id": "12345",
        "ipv4-address": "10.0.0.2/24",
        "ipv4-gateway": "10.0.0.1",
        "ipv4-subnet": "10.0.0.0/24",
        "ipv4-routes": [
            {"dst": "169.254.170.2/32"},
            {"dst": "169.254.170.0/20", "gw": "10.0.0.1"}
        ]
    }
}
```

## Parameters
* `id` (string, optional): information about this ip, can be any information related
to this ip.
* `ipv4-address` (string, optional): ipv4 address of the veth inside the
container network namespace.
* `ipv4-routes` (string, optional): list of routes to add to the container network
namespace. Each route is a dictionary with "dst" and optional "gw" fields. 
If "gw" is omitted, value of "gateway" will be used.
* `ipv4-gateway` (string, optional): IP inside of "subnet" to designate as the
gateway. Defaults to ".1" IP inside of the "subnet" block.
* `ipv4-subnet` (string, required): CIDR block for allocations.
*Note: either `id` or `ipv4-address` must be specified in delete operation.*

## Environment Variables
* `IPAM_DB_PATH` (string, optional): path of the boltdb file.
* `IPAM_DB_CONNECTION_TIMEOUT` (string, optional): timeout for the connection
to the boltdb.

## Example
Before running the command you should set up these environment variable:
* `CNI_COMMAND`: Command to execute eg: ADD.
* `CNI_PATH`: Plugin binary path eg: `pwd`/bin.
* `CNI_IFNAME`: Interface name inside the container, this is only required for
bridge plugin, but is hard coded in skel package which we consume. So for using
the ipam plugin separately, it should be set but won't be used.
Ref: https://github.com/containernetworking/cni/blob/v0.5.1/pkg/skel/skel.go#L53
### Add:
```
export CNI_COMMAND=ADD && cat mynet.conf | ../bin/ecs-ipam
```

### Del:
```
export CNI_COMMAND=DEL && cat mynet.conf | ../bin/ecs-ipam
```

`mynet.conf` is the configuration file for the plugin, it's the same as described
in the overview above.

Then you can use the following program to check the content of the db, be sure
to change the boltdb path and bucket name:
```golang
package main

import (
    "fmt"
    "github.com/docker/libkv"
    "github.com/docker/libkv/store"
    "github.com/libkv/store/boltdb"
    "time"
)

func init() {
    boltdb.Register()
}

func main() {
    db := "${BOLTDB_PATH}"
    bucket := "${BUCKET_NAME}"

    kv, err := libkv.NewStore(
        store.BOLTDB,
        []string{db},
        &store.Config{
            Bucket:            bucket,
            ConnectionTimeout: 10 * time.Second,
        },
    )
    if err != nil {
        fmt.Printf("Creating db failed: %v\n", err)
    }

    entries, err := kv.List("1")
    for _, pair := range entries {
        fmt.Printf("key=%v - value=%v\n", pair.Key, string(pair.Value))
    }
}
```
