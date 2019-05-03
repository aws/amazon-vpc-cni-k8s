#!/usr/bin/env bash
# Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
#       http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.
#
# This script generates a file in go with the license contents as a constant

# Set language to C to make sorting consistent among different environments.
export LANG=C

set -uo pipefail
LOG_DIR="/var/log/aws-routed-eni"
mkdir -p ${LOG_DIR}

# Collecting L-IPAMD introspection data
curl http://localhost:61679/v1/enis         > ${LOG_DIR}/eni.out
curl http://localhost:61679/v1/pods         > ${LOG_DIR}/pod.out
curl http://localhost:61679/v1/networkutils-env-settings > ${LOG_DIR}/networkutils-env.out
curl http://localhost:61679/v1/ipamd-env-settings > ${LOG_DIR}/ipamd-env.out
curl http://localhost:61679/v1/eni-configs  > ${LOG_DIR}/eni-configs.out

# Metrics
curl http://localhost:61678/metrics 2>&1 > ${LOG_DIR}/metrics.out

# Collecting kubelet introspection data
if [[ -n "${KUBECONFIG:-}" ]]; then
    command -v kubectl > /dev/null && kubectl get --kubeconfig=${KUBECONFIG} --raw=/api/v1/pods > ${LOG_DIR}/kubelet.out
elif [[ -f /etc/systemd/system/kubelet.service ]]; then
    KUBECONFIG=`grep kubeconfig /etc/systemd/system/kubelet.service | awk '{print $2}'`
    command -v kubectl > /dev/null && kubectl get --kubeconfig=${KUBECONFIG} --raw=/api/v1/pods > ${LOG_DIR}/kubelet.out
elif [[ -f /etc/eksctl/kubeconfig.yaml ]]; then
    command -v kubectl > /dev/null && kubectl get --kubeconfig=/etc/eksctl/kubeconfig.yaml --raw=/api/v1/pods > ${LOG_DIR}/kubelet.out
else
    echo "======== Unable to find KUBECONFIG, IGNORING POD DATA ========="
fi

# ifconfig
ifconfig > ${LOG_DIR}/ifconfig.out

# ip rule show
ip rule show > ${LOG_DIR}/iprule.out

# iptables-save
iptables-save > ${LOG_DIR}/iptables-save.out

# iptables -nvL
iptables -nvL > ${LOG_DIR}/iptables.out

# iptables -nvL -t nat
iptables -nvL -t nat > ${LOG_DIR}/iptables-nat.out

# iptables -nvL -t mangle
iptables -nvL -t mangle > ${LOG_DIR}/iptables-mangle.out

# dump cni config
mkdir -p ${LOG_DIR}/cni
cp /etc/cni/net.d/* ${LOG_DIR}/cni

# collect kubelet log
cp /var/log/messages ${LOG_DIR}/

# dump out route table
ROUTE_OUTPUT=${LOG_DIR}/"route.out"
echo "=============================================" >> ${ROUTE_OUTPUT}
echo "ip route show table all" >> ${ROUTE_OUTPUT}
ip route show table all >> ${ROUTE_OUTPUT}

# dump relevant sysctls
echo "================== sysctls ==================" > ${LOG_DIR}/sysctls.out
for f in /proc/sys/net/ipv4/conf/*/rp_filter; do
  echo "$f = $(cat ${f})" >> ${LOG_DIR}/sysctls.out
done

tar --exclude 'aws-cni-support.tar.gz' -cvzf ${LOG_DIR}/aws-cni-support.tar.gz ${LOG_DIR}/
