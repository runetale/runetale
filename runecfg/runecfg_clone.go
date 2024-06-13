package runecfg

import (
	"net/netip"

	"github.com/runetale/runetale/types/readonly"
)

type NodeView struct {
	ᚢ *Node
}

func (v NodeView) Addresses() readonly.Slice[netip.Prefix] { return readonly.SliceOf(v.ᚢ.Addresses) }

func (v NodeView) IsValid() bool { return v.ᚢ != nil }
