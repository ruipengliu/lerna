package host

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"google.golang.org/protobuf/proto"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/tasks"
)

// Catalog 是由受审查的参考适配器组成的能力目录：每项能力固定执行管理负责方和执行端点。
type Catalog struct {
	caps      map[string]tasks.Capability
	executors map[string]ports.Executor
	version   string
}

// NewCatalog 从执行适配器的声明建立目录。同一能力只能有一个适配器。
func NewCatalog(execs []ports.Executor, ledgerDomain, endpoint string) (*Catalog, error) {
	c := &Catalog{caps: map[string]tasks.Capability{}, executors: map[string]ports.Executor{}}
	h := sha256.New()
	var all []*lernav1.CapabilityDeclaration
	for _, e := range execs {
		if _, dup := c.executors[e.AdapterID()]; dup {
			return nil, fmt.Errorf("host: duplicate adapter %s", e.AdapterID())
		}
		c.executors[e.AdapterID()] = e
		for _, d := range e.Declarations() {
			if _, dup := c.caps[d.GetCapabilityId()]; dup {
				return nil, fmt.Errorf("host: duplicate capability %s", d.GetCapabilityId())
			}
			c.caps[d.GetCapabilityId()] = tasks.Capability{Decl: d, AdapterID: e.AdapterID(), LedgerDomainID: ledgerDomain, EndpointID: endpoint}
			all = append(all, d)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].GetCapabilityId() < all[j].GetCapabilityId() })
	for _, d := range all {
		b, err := proto.MarshalOptions{Deterministic: true}.Marshal(d)
		if err != nil {
			return nil, err
		}
		h.Write(b)
	}
	c.version = hex.EncodeToString(h.Sum(nil))[:16]
	return c, nil
}

// Lookup 返回能力。
func (c *Catalog) Lookup(id string) (tasks.Capability, bool) {
	v, ok := c.caps[id]
	return v, ok
}

// All 返回全部能力声明。
func (c *Catalog) All() []*lernav1.CapabilityDeclaration {
	var out []*lernav1.CapabilityDeclaration
	for _, v := range c.caps {
		out = append(out, v.Decl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GetCapabilityId() < out[j].GetCapabilityId() })
	return out
}

// Version 返回目录版本。
func (c *Catalog) Version() string { return c.version }

// Executor 返回适配器。
func (c *Catalog) Executor(adapterID string) (ports.Executor, bool) {
	e, ok := c.executors[adapterID]
	return e, ok
}
