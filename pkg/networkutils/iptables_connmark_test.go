package networkutils

import (
	"fmt"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	mock_iptables "github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper/mocks"
	"github.com/coreos/go-iptables/iptables"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"

	"github.com/golang/mock/gomock"
)

func TestIptablesConnmarkSetup(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockIpt := mock_iptables.NewMockIptables()
	connmark := &iptablesConnmark{
		vethPrefix: "eni",
		mark:       0x80,
		newIptables: func(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIpt, nil
		},
	}
	exemptCIDRs := []string{"10.0.1.1/20", "10.0.2.2/20"}
	err := connmark.Setup(exemptCIDRs)
	assert.NoError(t, err)
	// Verify chain was created
	exists, _ := mockIpt.ChainExists("nat", connmarkChainName)
	assert.True(t, exists)

	//Verify Jump Rule Exists
	exists, _ = mockIpt.Exists("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", connmarkChainName)
	assert.True(t, exists)

	// Verify Cidr rules
	for _, cidr := range exemptCIDRs {
		exists, _ := mockIpt.Exists("nat", connmarkChainName, "-d", cidr, "-m", "comment", "--comment", "AWS CONNMARK CHAIN", "-j", "RETURN")
		assert.True(t, exists)
	}

	// Verify Restore Mark Exists
	exists, _ = mockIpt.Exists("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", uint32(0x80)))
	assert.True(t, exists)

	// Verify set mark rule exists
	exists, _ = mockIpt.Exists("nat", connmarkChainName, "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", fmt.Sprintf("%#x/%#x", uint32(0x80), uint32(0x80)))
	assert.True(t, exists)

}

func TestIptablesConnmarkSetup_NewIptablesError(t *testing.T) {
	connmark := &iptablesConnmark{
		vethPrefix: "eni",
		mark:       0x80,
		newIptables: func(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return nil, errors.New("iptables init failed")
		},
	}

	err := connmark.Setup([]string{"10.0.0.0/8"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "iptables init failed")
}

func TestIptablesConnmarkSetup_EmptyCIDRs(t *testing.T) {
	connmark := &iptablesConnmark{
		vethPrefix: "eni",
		mark:       0x80,
		newIptables: func(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mock_iptables.NewMockIptables(), nil
		},
	}

	err := connmark.Setup([]string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exemptCIDRs cannot be empty")
}
func TestIptablesConnmarkSetup_StaleRulesRemoved(t *testing.T) {
	mockIpt := mock_iptables.NewMockIptables()
	connmark := &iptablesConnmark{
		vethPrefix: "eni",
		mark:       0x80,
		newIptables: func(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIpt, nil
		},
	}

	// Setup with initial CIDRs
	err := connmark.Setup([]string{"10.0.0.0/8", "172.16.0.0/12"})
	assert.NoError(t, err)

	// Verify both CIDRs exist
	exists, _ := mockIpt.Exists("nat", connmarkChainName, "-d", "10.0.0.0/8", "-m", "comment", "--comment", "AWS CONNMARK CHAIN", "-j", "RETURN")
	assert.True(t, exists)
	exists, _ = mockIpt.Exists("nat", connmarkChainName, "-d", "172.16.0.0/12", "-m", "comment", "--comment", "AWS CONNMARK CHAIN", "-j", "RETURN")
	assert.True(t, exists)

	// Setup again with only one CIDR
	err = connmark.Setup([]string{"10.0.0.0/8"})
	assert.NoError(t, err)

	// Verify stale CIDR is removed
	exists, _ = mockIpt.Exists("nat", connmarkChainName, "-d", "172.16.0.0/12", "-m", "comment", "--comment", "AWS CONNMARK CHAIN", "-j", "RETURN")
	assert.False(t, exists)

	// Verify remaining CIDR still exists
	exists, _ = mockIpt.Exists("nat", connmarkChainName, "-d", "10.0.0.0/8", "-m", "comment", "--comment", "AWS CONNMARK CHAIN", "-j", "RETURN")
	assert.True(t, exists)
}

func TestIptablesConnmarkSetup_LegacyRuleDeleted(t *testing.T) {
	mockIpt := mock_iptables.NewMockIptables()

	// Pre-add legacy rule with state match
	_ = mockIpt.NewChain("nat", connmarkChainName)
	legacyRule := []string{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", connmarkChainName}
	_ = mockIpt.Append("nat", "PREROUTING", legacyRule...)

	// Verify legacy rule exists
	exists, _ := mockIpt.Exists("nat", "PREROUTING", legacyRule...)
	assert.True(t, exists)

	connmark := &iptablesConnmark{
		vethPrefix: "eni",
		mark:       0x80,
		newIptables: func(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIpt, nil
		},
	}

	err := connmark.Setup([]string{"10.0.0.0/8"})
	assert.NoError(t, err)

	// Verify legacy rule is deleted
	exists, _ = mockIpt.Exists("nat", "PREROUTING", legacyRule...)
	assert.False(t, exists)

	// Verify new jump rule exists (without state match)
	exists, _ = mockIpt.Exists("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", connmarkChainName)
	assert.True(t, exists)
}
