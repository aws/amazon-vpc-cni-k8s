#!/bin/bash
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

set -e
LOG_DIR="/var/log/aws-routed-eni"

# collecting L-IPAMD introspection data
curl http://localhost:61678/v1/enis         > ${LOG_DIR}/eni.output
curl http://localhost:61678/v1/pods         > ${LOG_DIR}/pod.output
curl http://localhost:61678/v1/networkutils-env-settings > ${LOG_DIR}/networkutils-env.output
curl http://localhost:61678/v1/ipamd-env-settings > ${LOG_DIR}/ipamd-env.output
curl http://localhost:61678/v1/eni-configs  > ${LOG_DIR}/eni-configs.output

# metrics TODO not able to use LOG_DIR
curl http://localhost:61678/metrics 2>&1 > /var/log/aws-routed-eni/metrics.output

# collecting kubelet introspection data
curl http://localhost:10255/pods  > ${LOG_DIR}/kubelet.output

# ifconfig
ifconfig > ${LOG_DIR}/ifconig.output

# ip rule show
ip rule show > ${LOG_DIR}/iprule.output

# iptables-save
iptables-save > $LOG_DIR/iptables-save.out

# iptables -nvL
iptables -nvL > $LOG_DIR/iptables.out

# iptables -nvL -t nat
iptables -nvL -t nat > $LOG_DIR/iptables-nat.out

# dump cni config
mkdir -p $LOG_DIR/cni
cp /etc/cni/net.d/* $LOG_DIR/cni

# collect kubelet log
cp /var/log/messages $LOG_DIR/

# dump out route table
ROUTE_OUTPUT="route.output"
echo "=============================================" >> ${LOG_DIR}/${ROUTE_OUTPUT}
echo "ip route show table all" >> $LOG_DIR/$ROUTE_OUTPUT
ip route show table all >> $LOG_DIR/$ROUTE_OUTPUT

# dump relevant sysctls
echo "================== sysctls ==================" > ${LOG_DIR}/sysctls.out
for f in /proc/sys/net/ipv4/conf/{all,default,eth0}/rp_filter; do
  echo "$f = $(cat $f)" >> ${LOG_DIR}/sysctls.out
done

tar -cvzf $LOG_DIR/aws-cni-support.tar.gz ${LOG_DIR}/
