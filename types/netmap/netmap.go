package netmap

import (
	"net/netip"

	"github.com/runetale/runetale/runecfg"
	"github.com/runetale/runetale/types/ipproto"
	"github.com/runetale/runetale/types/key"
	"github.com/runetale/runetale/types/readonly"
)

// 最終的にgrpcから構造体にマッピングされる
type NetworkMap struct {
	SelfNode runecfg.NodeView
	// This NodeKey
	NodeKey key.NodePublic
	// Private Key that this node has
	PrivateKey key.NodePrivateKey

	// 接続するPeerのNode, readonly
	Peers []runecfg.NodeView

	// Filter Rulesをserverから受け取りPacket Filterにセットされる
	// dstsはsrcから一方通行でしかアクセスできないので、srcのポートはいらない
	// Packet Filter 最終的にtundev.SetFilterに入る
	PacketFilter []Match

	// このNodeのDNSの名前
	DNSName string

	// runenet name
	Domain string
}

func (nm *NetworkMap) GetAddresses() readonly.Slice[netip.Prefix] {
	return nm.SelfNode.Addresses()
}

// for the packet filter
type PortRange struct {
	First, Last uint16 // inclusive
}

type NetPortRange struct {
	Net   netip.Prefix
	Ports PortRange
}

type Match struct {
	IPProto []ipproto.Proto
	Srcs    []netip.Prefix
	Dsts    []NetPortRange
}
