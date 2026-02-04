package nft

import "github.com/google/nftables"

type Client interface {
	AddRule(r *nftables.Rule) *nftables.Rule
	InsertRule(r *nftables.Rule) *nftables.Rule
	DelRule(r *nftables.Rule) error
	GetRules(t *nftables.Table, c *nftables.Chain) ([]*nftables.Rule, error)
	ListChain(table *nftables.Table, chain string) (*nftables.Chain, error)
	AddChain(c *nftables.Chain) *nftables.Chain
	FlushChain(c *nftables.Chain)
	DelChain(c *nftables.Chain)
	AddTable(t *nftables.Table) *nftables.Table
	DelTable(t *nftables.Table)
	Flush() error
}

var _ Client = (*client)(nil)

func New() (Client, error) {
	conn, err := nftables.New()
	if err != nil {
		return nil, err
	}
	return &client{conn: conn}, nil
}

type client struct {
	conn *nftables.Conn
}

func (c *client) AddRule(r *nftables.Rule) *nftables.Rule {
	return c.conn.AddRule(r)
}

func (c *client) InsertRule(r *nftables.Rule) *nftables.Rule {
	return c.conn.InsertRule(r)
}

func (c *client) DelRule(r *nftables.Rule) error {
	return c.conn.DelRule(r)
}

func (c *client) GetRules(t *nftables.Table, chain *nftables.Chain) ([]*nftables.Rule, error) {
	return c.conn.GetRules(t, chain)
}

func (c *client) ListChain(table *nftables.Table, chain string) (*nftables.Chain, error) {
	return c.conn.ListChain(table, chain)
}

func (c *client) AddChain(chain *nftables.Chain) *nftables.Chain {
	return c.conn.AddChain(chain)
}

func (c *client) FlushChain(chain *nftables.Chain) {
	c.conn.FlushChain(chain)
}

func (c *client) DelChain(chain *nftables.Chain) {
	c.conn.DelChain(chain)
}

func (c *client) AddTable(t *nftables.Table) *nftables.Table {
	return c.conn.AddTable(t)
}

func (c *client) DelTable(t *nftables.Table) {
	c.conn.DelTable(t)
}

func (c *client) Flush() error {
	return c.conn.Flush()
}
