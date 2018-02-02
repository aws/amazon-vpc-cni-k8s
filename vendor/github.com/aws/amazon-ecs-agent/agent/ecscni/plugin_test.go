package ecscni

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/ecscni/mocks_cnitypes"
	"github.com/aws/amazon-ecs-agent/agent/ecscni/mocks_libcni"
	"github.com/containernetworking/cni/libcni"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestSetupNS(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ecscniClient := NewClient(&Config{})
	libcniClient := mock_libcni.NewMockCNI(ctrl)
	ecscniClient.(*cniClient).libcni = libcniClient

	mockResult := mock_types.NewMockResult(ctrl)

	additionalRoutesJson := `["169.254.172.1/32", "10.11.12.13/32"]`
	var additionalRoutes []cnitypes.IPNet
	err := json.Unmarshal([]byte(additionalRoutesJson), &additionalRoutes)
	assert.NoError(t, err)

	gomock.InOrder(
		libcniClient.EXPECT().AddNetworkList(gomock.Any(), gomock.Any()).Return(mockResult, nil).Do(func(net *libcni.NetworkConfigList, rt *libcni.RuntimeConf) {
			assert.Len(t, net.Plugins, 2, "expected 2 plugins for SetupNS")
			bridgePlugin := net.Plugins[0]
			assert.Equal(t, ECSBridgePluginName, bridgePlugin.Network.Type, "first plugin should be bridge")
			var bridgeConfig BridgeConfig
			err := json.Unmarshal(bridgePlugin.Bytes, &bridgeConfig)
			assert.NoError(t, err, "unmarshal BridgeConfig")
			assert.Len(t, bridgeConfig.IPAM.IPV4Routes, 3, "default route plus two extra routes")
		}),
		mockResult.EXPECT().String().Return(""),
	)

	err = ecscniClient.SetupNS(&Config{AdditionalLocalRoutes: additionalRoutes})
	assert.NoError(t, err)
}

func TestCleanupNS(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ecscniClient := NewClient(&Config{})
	libcniClient := mock_libcni.NewMockCNI(ctrl)
	ecscniClient.(*cniClient).libcni = libcniClient

	libcniClient.EXPECT().DelNetworkList(gomock.Any(), gomock.Any()).Return(nil)

	additionalRoutesJson := `["169.254.172.1/32", "10.11.12.13/32"]`
	var additionalRoutes []cnitypes.IPNet
	err := json.Unmarshal([]byte(additionalRoutesJson), &additionalRoutes)
	assert.NoError(t, err)
	err = ecscniClient.CleanupNS(&Config{AdditionalLocalRoutes: additionalRoutes})
	assert.NoError(t, err)
}

func TestReleaseIPInIPAM(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ecscniClient := NewClient(&Config{})
	libcniClient := mock_libcni.NewMockCNI(ctrl)
	ecscniClient.(*cniClient).libcni = libcniClient

	libcniClient.EXPECT().DelNetworkList(gomock.Any(), gomock.Any()).Return(nil)

	err := ecscniClient.ReleaseIPResource(&Config{})
	assert.NoError(t, err)
}

// TestConstructNetworkConfig tests constructNetworkConfig creates the correct
// configuration for bridge/eni plugin
func TestConstructNetworkConfigWithoutIPAM(t *testing.T) {
	ecscniClient := NewClient(&Config{})

	config := &Config{
		ENIID:                "eni-12345678",
		ContainerID:          "containerid12",
		ContainerPID:         "pid",
		ENIIPV4Address:       "172.31.21.40",
		ENIIPV6Address:       "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
		ENIMACAddress:        "02:7b:64:49:b1:40",
		BridgeName:           "bridge-test1",
		BlockInstanceMetdata: true,
	}

	networkConfigList, err := ecscniClient.(*cniClient).createNetworkConfig(config, ecscniClient.(*cniClient).createBridgeNetworkConfigWithoutIPAM)
	assert.NoError(t, err, "construct cni plugins configuration failed")

	bridgeConfig := &BridgeConfig{}
	eniConfig := &ENIConfig{}
	for _, plugin := range networkConfigList.Plugins {
		var err error
		if plugin.Network.Type == ECSBridgePluginName {
			err = json.Unmarshal(plugin.Bytes, bridgeConfig)
		} else if plugin.Network.Type == ECSENIPluginName {
			err = json.Unmarshal(plugin.Bytes, eniConfig)
		}
		assert.NoError(t, err, "unmarshal config from bytes failed")
	}

	assert.Equal(t, config.BridgeName, bridgeConfig.BridgeName)
	assert.Equal(t, IPAMConfig{}, bridgeConfig.IPAM)
	assert.Equal(t, config.ENIID, eniConfig.ENIID)
	assert.Equal(t, config.ENIIPV4Address, eniConfig.IPV4Address)
	assert.Equal(t, config.ENIIPV6Address, eniConfig.IPV6Address)
	assert.Equal(t, config.ENIMACAddress, eniConfig.MACAddress)
	assert.True(t, eniConfig.BlockInstanceMetdata)
}

// TestConstructNetworkConfig tests constructNetworkConfig creates the correct
// configuration for bridge/eni/ipam plugin
func TestConstructNetworkConfig(t *testing.T) {
	ecscniClient := NewClient(&Config{})

	additionalRoutesJson := `["169.254.172.1/32", "10.11.12.13/32"]`
	var additionalRoutes []cnitypes.IPNet
	err := json.Unmarshal([]byte(additionalRoutesJson), &additionalRoutes)
	assert.NoError(t, err)

	config := &Config{
		ENIID:                 "eni-12345678",
		ContainerID:           "containerid12",
		ContainerPID:          "pid",
		ENIIPV4Address:        "172.31.21.40",
		ENIIPV6Address:        "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
		ENIMACAddress:         "02:7b:64:49:b1:40",
		BridgeName:            "bridge-test1",
		BlockInstanceMetdata:  true,
		AdditionalLocalRoutes: additionalRoutes,
	}

	networkConfigList, err := ecscniClient.(*cniClient).createNetworkConfig(config, ecscniClient.(*cniClient).createBridgeNetworkConfigWithIPAM)
	assert.NoError(t, err, "construct cni plugins configuration failed")

	bridgeConfig := &BridgeConfig{}
	eniConfig := &ENIConfig{}
	for _, plugin := range networkConfigList.Plugins {
		var err error
		switch plugin.Network.Type {
		case ECSBridgePluginName:
			err = json.Unmarshal(plugin.Bytes, bridgeConfig)
		case ECSENIPluginName:
			err = json.Unmarshal(plugin.Bytes, eniConfig)
		}
		assert.NoError(t, err, "unmarshal config from bytes failed for plugin %s\n%s", plugin.Network.Type, string(plugin.Bytes))
	}

	assert.Equal(t, config.BridgeName, bridgeConfig.BridgeName)
	assert.Equal(t, ecsSubnet, bridgeConfig.IPAM.IPV4Subnet)
	assert.Equal(t, TaskIAMRoleEndpoint, bridgeConfig.IPAM.IPV4Routes[0].Dst.String())
	assert.Equal(t, config.ENIID, eniConfig.ENIID)
	assert.Equal(t, config.ENIIPV4Address, eniConfig.IPV4Address)
	assert.Equal(t, config.ENIIPV6Address, eniConfig.IPV6Address)
	assert.Equal(t, config.ENIMACAddress, eniConfig.MACAddress)
	assert.True(t, eniConfig.BlockInstanceMetdata)
}

func TestCNIPluginVersion(t *testing.T) {
	testCases := []struct {
		version *cniPluginVersion
		str     string
	}{
		{
			version: &cniPluginVersion{
				Version: "1",
				Dirty:   false,
				Hash:    "hash",
			},
			str: "hash-1",
		},
		{
			version: &cniPluginVersion{
				Version: "1",
				Dirty:   true,
				Hash:    "hash",
			},
			str: "@hash-1",
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("version string %s", tc.str), func(t *testing.T) {
			assert.Equal(t, tc.str, tc.version.str())
		})
	}
}
