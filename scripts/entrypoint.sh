#!/usr/bin/env bash

# NOTE(jaypipes): Normally, we would prefer *not* to have an entrypoint script
# and instead just start the agent daemon as the container's CMD. However, the
# design of CNI is such that Kubelet looks for the presence of binaries and CNI
# configuration files in specific directories, and the presence of those files
# is the trigger to Kubelet that that particular CNI plugin is "ready".
#
# In the case of the AWS VPC CNI plugin, we have two components to the plugin.
# The first component is the actual CNI binary that is execve'd from Kubelet
# when a container is started or destroyed. The second component is the
# aws-k8s-agent daemon which houses the IPAM controller.
#
# As mentioned above, Kubelet considers a CNI plugin "ready" when it sees the
# binary and configuration file for the plugin in a well-known directory. For
# the AWS VPC CNI plugin binary, we only want to copy the CNI plugin binary
# into that well-known directory AFTER we have succeessfully started the IPAM
# daemon and know that it can connect to Kubernetes and the local EC2 metadata
# service. This is why this entrypoint script exists; we start the IPAM daemon
# and wait until we know it is up and running successfully before copying the
# CNI plugin binary and its configuration file to the well-known directory that
# Kubelet looks in.

# turn on exit on subprocess error and exit on undefined variables
set -eu
# turn on bash's job control
set -m

# Check for all the required binaries before we go forward
if [ ! -f aws-k8s-agent ]; then
    echo "Required aws-k8s-agent executable not found."
    exit 1
fi
if [ ! -f grpc-health-probe ]; then
    echo "Required grpc-health-probe executable not found."
    exit 1
fi

AGENT_LOG_PATH=${AGENT_LOG_PATH:-"aws-k8s-agent.log"}
HOST_CNI_BIN_PATH=${HOST_CNI_BIN_PATH:-"/host/opt/cni/bin"}
HOST_CNI_CONFDIR_PATH=${HOST_CNI_CONFDIR_PATH:-"/host/etc/cni/net.d"}
AWS_VPC_K8S_CNI_VETHPREFIX=${AWS_VPC_K8S_CNI_VETHPREFIX:-"eni"}
AWS_VPC_ENI_MTU=${AWS_VPC_ENI_MTU:-"9001"}
AWS_VPC_K8S_PLUGIN_LOG_FILE=${AWS_VPC_K8S_PLUGIN_LOG_FILE:-"/var/log/aws-routed-eni/plugin.log"}
AWS_VPC_K8S_PLUGIN_LOG_LEVEL=${AWS_VPC_K8S_PLUGIN_LOG_LEVEL:-"Debug"}

AWS_VPC_K8S_CNI_CONFIGURE_RPFILTER=${AWS_VPC_K8S_CNI_CONFIGURE_RPFILTER:-"true"}

# Checks for IPAM connectivity on localhost port 50051, retrying connectivity
# check with a timeout of 36 seconds
wait_for_ipam() {
    local __sleep_time=0

    until [ $__sleep_time -eq 8 ]; do
        sleep $((__sleep_time++))
        if ./grpc-health-probe -addr 127.0.0.1:50051 >/dev/null 2>&1; then
            return 0
        fi
    done
    return 1
}

# If there is no init container, copy the required files
if [[ "$AWS_VPC_K8S_CNI_CONFIGURE_RPFILTER" != "false" ]]; then
    # Copy files
    echo "Copying CNI plugin binaries ... "
    PLUGIN_BINS="portmap aws-cni-support.sh"
    for b in $PLUGIN_BINS; do
        # If the file exist, delete it first
        if [[ -f "$HOST_CNI_BIN_PATH/$b" ]]; then
            rm "$HOST_CNI_BIN_PATH/$b"
        fi
        cp "$b" "$HOST_CNI_BIN_PATH"
    done
fi

echo -n "Starting IPAM daemon in the background ... "
./aws-k8s-agent | tee -i "$AGENT_LOG_PATH" 2>&1 &
echo "ok."

echo -n "Checking for IPAM connectivity ... "

if ! wait_for_ipam; then
    echo " failed."
    echo "Timed out waiting for IPAM daemon to start:"
    cat "$AGENT_LOG_PATH" >&2
    exit 1
fi

echo "ok."

echo -n "Copying CNI plugin binary and config file ... "

cp aws-cni "$HOST_CNI_BIN_PATH"

sed -i s~__VETHPREFIX__~"${AWS_VPC_K8S_CNI_VETHPREFIX}"~g 10-aws.conflist
sed -i s~__MTU__~"${AWS_VPC_ENI_MTU}"~g 10-aws.conflist
sed -i s~__PLUGINLOGFILE__~"${AWS_VPC_K8S_PLUGIN_LOG_FILE}"~g 10-aws.conflist
sed -i s~__PLUGINLOGLEVEL__~"${AWS_VPC_K8S_PLUGIN_LOG_LEVEL}"~g 10-aws.conflist
cp 10-aws.conflist "$HOST_CNI_CONFDIR_PATH"

echo "ok."

if [[ -f "$HOST_CNI_CONFDIR_PATH/aws.conf" ]]; then
    rm "$HOST_CNI_CONFDIR_PATH/aws.conf"
fi

# Bring the aws-k8s-agent process back into the foreground
echo "Foregrounding IPAM daemon ... "
fg %1 >/dev/null 2>&1 || { echo "failed (process terminated)" && cat "$AGENT_LOG_PATH" && exit 1; }
