package networkutils

import (
	"github.com/coreos/go-iptables/iptables"
	"strings"
)

// goIPTables directly wraps methods exposed by the "github.com/coreos/go-iptables/iptables" package
type goIPTables interface {
	Exists(table, chain string, rulespec ...string) (bool, error)
	Insert(table, chain string, pos int, rulespec ...string) error
	Append(table, chain string, rulespec ...string) error
	Delete(table, chain string, rulespec ...string) error
	List(table, chain string) ([]string, error)
	NewChain(table, chain string) error
	ClearChain(table, chain string) error
	DeleteChain(table, chain string) error
	ListChains(table string) ([]string, error)
	HasRandomFully() bool
}

type IPTables interface {
	goIPTables
}

var _ IPTables = &ipTables{}

type ipTables struct {
	*iptables.IPTables
}

// NewIPTablesWithProtocol creates a new ipTables object
func NewIPTablesWithProtocol(proto iptables.Protocol) (IPTables, error) {
	ipt, err := iptables.NewWithProtocol(proto)
	if err != nil {
		return nil, err
	}
	return &ipTables{IPTables: ipt}, nil
}

func IsChainExistErr(err error) bool {
	return strings.Contains(err.Error(), "Chain already exists")
}
