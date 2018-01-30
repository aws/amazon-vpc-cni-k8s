# Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
#	http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.

$oldActionPref = $ErrorActionPreference
$ErrorActionPreference = 'Continue'

# This setup script is required in order to enable task IAM roles on windows instances.

# 169.254.170.2:51679 is the IP address used for task IAM roles.
$credentialAddress = "169.254.170.2"
$credentialPort = "51679"
$loopbackAddress = "127.0.0.1"

$adapter = (Get-NetAdapter -Name "*APIPA*")
if(!($adapter)) {
	# APIPA nat adapter controls IP range 169.254.x.x on windows.
	Add-VMNetworkAdapter -VMNetworkAdapterName "APIPA" -SwitchName nat -ManagementOS
}

$ifIndex = (Get-NetAdapter -Name "*APIPA*" | Sort-Object | Select ifIndex).ifIndex

$dockerSubnet = (docker network inspect nat | ConvertFrom-Json).IPAM.Config.Subnet

# This address will only exist on systems that have already set up the routes.
$ip = (Get-NetRoute -InterfaceIndex $ifIndex -DestinationPrefix $dockerSubnet)
if(!($ip)) {

	# This command tells the APIPA interface that this IP exists.
	New-NetIPAddress -InterfaceIndex $ifIndex -IPAddress $credentialAddress -PrefixLength 32

	# Enable the default docker IP range to be routable by the APIPA interface.
	New-NetRoute -DestinationPrefix $dockerSubnet -ifIndex $ifindex

	# Exposes credential port for local windows firewall
	New-NetFirewallRule -DisplayName "Allow Inbound Port $credentialPort" -Direction Inbound -LocalPort $credentialPort -Protocol TCP -Action Allow

	# This forwards traffic from port 80 and listens on the IAM role IP address.
	# 'portproxy' doesn't have a powershell module equivalent, but we could move if it becomes available.
	netsh interface portproxy add v4tov4 listenaddress=$credentialAddress listenport=80 connectaddress=$loopbackAddress connectport=$credentialPort
}

$ErrorActionPreference=$oldActionPref
