#!/usr/bin/env bash

set -euxo pipefail

PLUGIN_BINS="loopback portmap aws-cni-support.sh"

for b in $PLUGIN_BINS; do
    if [ ! -f "$b" ]; then
        echo "Required $b executable not found."
        exit 1
    fi
done

HOST_CNI_BIN_PATH=${HOST_CNI_BIN_PATH:-"/host/opt/cni/bin"}

# Copy files
echo "Copying CNI plugin binaries ... "

for b in $PLUGIN_BINS; do
    # If the file exist, delete it first
    if [[ -f "$HOST_CNI_BIN_PATH/$b" ]]; then
        rm "$HOST_CNI_BIN_PATH/$b"
    fi
    cp "$b" "$HOST_CNI_BIN_PATH"
done

# Configure rp_filter
echo "Configure rp_filter loose... "
TOKEN=$(curl -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 60")
HOST_IP=$(curl -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/local-ipv4)
PRIMARY_IF=$(ip -4 -o a | grep "$HOST_IP" | awk '{print $2}')
sysctl -w "net.ipv4.conf.$PRIMARY_IF.rp_filter=2"

cat "/proc/sys/net/ipv4/conf/$PRIMARY_IF/rp_filter"

echo "CNI init container done"
