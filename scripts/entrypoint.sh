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
# into that well-known directory AFTER we have successfully started the IPAM
# daemon and know that it can connect to Kubernetes and the local EC2 metadata
# service. This is why this entrypoint script exists; we start the IPAM daemon
# and wait until we know it is up and running successfully before copying the
# CNI plugin binary and its configuration file to the well-known directory that
# Kubelet looks in.

# turn on exit on subprocess error and exit on undefined variables
set -eu
# turn on bash's job control
set -m

log_in_json() 
{
    FILENAME="${0##*/}"
    LOGTYPE=$1
    MSG=$2
    TIMESTAMP=$(date +%FT%T.%3NZ)
    printf '{"level":"%s","ts":"%s","caller":"%s","msg":"%s"}\n' "$LOGTYPE" "$TIMESTAMP" "$FILENAME" "$MSG"
}

validate_env_var()
{
    log_in_json info "Validating env variables ..."
    if [[ "${AWS_VPC_K8S_PLUGIN_LOG_FILE,,}" == "stdout" ]]; then
        log_in_json error "AWS_VPC_K8S_PLUGIN_LOG_FILE cannot be set to stdout"
        exit 1     
    fi 

    if [[ ${#AWS_VPC_K8S_CNI_VETHPREFIX} -gt 4 ]]; then
        log_in_json error "AWS_VPC_K8S_CNI_VETHPREFIX cannot be longer than 4 characters"
        exit 1
    fi

    case ${AWS_VPC_K8S_CNI_VETHPREFIX} in
        eth|vlan|lo)
          log_in_json error "AWS_VPC_K8S_CNI_VETHPREFIX cannot be set to reserved values eth or vlan or lo"
          exit 1
          ;;
    esac
}

# Check for all the required binaries before we go forward
if [ ! -f aws-k8s-agent ]; then
    log_in_json error "Required aws-k8s-agent executable not found."
    exit 1
fi
if [ ! -f grpc-health-probe ]; then
    log_in_json error "Required grpc-health-probe executable not found."
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

validate_env_var

# Check for ipamd connectivity on localhost port 50051
wait_for_ipam() {
    while :
    do
        if ./grpc-health-probe -addr 127.0.0.1:50051 >/dev/null 2>&1; then
            return 0
        fi
        # We sleep for 1 second between each retry
        sleep 1
    done
}

# If there is no init container, copy the required files
if [[ "$AWS_VPC_K8S_CNI_CONFIGURE_RPFILTER" != "false" ]]; then
    # Copy files
    log_in_json info "Copying CNI plugin binaries ... "
    PLUGIN_BINS="loopback portmap bandwidth aws-cni-support.sh"
    for b in $PLUGIN_BINS; do
        # Install the binary
        install "$b" "$HOST_CNI_BIN_PATH"
    done
fi

log_in_json info "Install CNI binary.."
install aws-cni "$HOST_CNI_BIN_PATH"

log_in_json info "Starting IPAM daemon in the background ... "
./aws-k8s-agent | tee -i "$AGENT_LOG_PATH" 2>&1 &

log_in_json info "Checking for IPAM connectivity ... "

if ! wait_for_ipam; then
    log_in_json error "Timed out waiting for IPAM daemon to start:"
    cat "$AGENT_LOG_PATH" >&2
    exit 1
fi

log_in_json info "Copying config file ... "

# modify the static config to populate it with the env vars
sed \
  -e s~__VETHPREFIX__~"${AWS_VPC_K8S_CNI_VETHPREFIX}"~g \
  -e s~__MTU__~"${AWS_VPC_ENI_MTU}"~g \
  -e s~__PLUGINLOGFILE__~"${AWS_VPC_K8S_PLUGIN_LOG_FILE}"~g \
  -e s~__PLUGINLOGLEVEL__~"${AWS_VPC_K8S_PLUGIN_LOG_LEVEL}"~g \
  10-aws.conflist > "$HOST_CNI_CONFDIR_PATH/10-aws.conflist"

log_in_json info "Successfully copied CNI plugin binary and config file."

if [[ -f "$HOST_CNI_CONFDIR_PATH/aws.conf" ]]; then
    rm "$HOST_CNI_CONFDIR_PATH/aws.conf"
fi

# Bring the aws-k8s-agent process back into the foreground
log_in_json info "Foregrounding IPAM daemon ..."
fg %1 >/dev/null 2>&1 || { log_in_json error "failed (process terminated)" && cat "$AGENT_LOG_PATH" && exit 1; }
