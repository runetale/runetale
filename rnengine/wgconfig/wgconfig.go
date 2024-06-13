package wgconfig

import (
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"

	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/types/key"
)

type WgConfig struct {
	Name string
	// このNodeのPrivateKey
	PrivateKey key.NodePrivateKey
	// このNodeのAddress
	Addresses []netip.Prefix
	MTU       uint16

	// wgtypes.PeerConfigで使うRemote Peers
	// 接続先のPeerの配列
	Peers []Peer
}

type Peer struct {
	PublicKey key.NodePublic
	// networkmapのpeersから受け取る
	// bartに格納される
	AllowedIPs []netip.Prefix

	// nilでない場合、このアドレスを使ってこのピアへのIPv4トラフィックをマスカレードする
	// このアドレスを使って、このPeerと通信を行う
	// マスカレードは内部ネットワークの構成やデバイスのIPアドレスを隠す
	// つまりそのIPアドレスからアクセスが来たように見える
	V4MasqAddr *netip.Addr

	// もしtrueならこのノードは牢獄に入れられ、接続の開始を許可されるべきではないことを示す
	// しかしこのノードへのアウトバウンド接続は許可される
	IsJailed            bool
	PersistentKeepalive uint16
	// ice endpoint?
	Endpoint *net.UDPAddr
}

// PeerWithKey returns the Peer with key k and reports whether it was found.
func (config WgConfig) PeerWithKey(k key.NodePublic) (Peer, bool) {
	for _, p := range config.Peers {
		if p.PublicKey == k {
			return p, true
		}
	}
	return Peer{}, false
}

// ConfigからWireGuardのDeviceの設定を行う処理たち
func (cfg *WgConfig) ToUAPI(logger *log.Logger, w io.Writer, prev *WgConfig) error {
	var stickyErr error
	set := func(key, value string) {
		if stickyErr != nil {
			return
		}
		_, err := fmt.Fprintf(w, "%s=%s\n", key, value)
		if err != nil {
			stickyErr = err
		}
	}
	setUint16 := func(key string, value uint16) {
		set(key, strconv.FormatUint(uint64(value), 10))
	}
	setPeer := func(peer Peer) {
		set("public_key", peer.PublicKey.UntypedHexString())
	}

	// Device設定
	if !prev.PrivateKey.Equal(cfg.PrivateKey) {
		set("private_key", cfg.PrivateKey.UntypedHexString())
	}

	old := make(map[key.NodePublic]Peer)
	for _, p := range prev.Peers {
		old[p.PublicKey] = p
	}

	// 全てのPeerの追加と設定
	for _, p := range cfg.Peers {
		oldPeer, wasPresent := old[p.PublicKey]

		willSetEndpoint := oldPeer.Endpoint != p.Endpoint || !wasPresent
		willChangeIPs := !cidrsEqual(oldPeer.AllowedIPs, p.AllowedIPs) || !wasPresent
		willChangeKeepalive := oldPeer.PersistentKeepalive != p.PersistentKeepalive // if not wasPresent, no need to redundantly set zero (default)

		if !willSetEndpoint && !willChangeIPs && !willChangeKeepalive {
			continue
		}

		setPeer(p)
		set("protocol_version", "1")

		if willSetEndpoint {
			if wasPresent {
				logger.Logger.Infof("[unexpected] endpoint changed from %s to %s", oldPeer.Endpoint, p.PublicKey)
			}
			set("endpoint", p.PublicKey.UntypedHexString())
		}

		if willChangeIPs {
			set("replace_allowed_ips", "true")
			for _, ipp := range p.AllowedIPs {
				set("allowed_ip", ipp.String())
			}
		}

		if willChangeKeepalive {
			setUint16("persistent_keepalive_interval", p.PersistentKeepalive)
		}
	}

	for _, p := range cfg.Peers {
		delete(old, p.PublicKey)
	}
	for _, p := range old {
		setPeer(p)
		set("remove", "true")
	}

	if stickyErr != nil {
		stickyErr = fmt.Errorf("ToUAPI: %w", stickyErr)
	}
	return stickyErr
}

func cidrsEqual(x, y []netip.Prefix) bool {
	if len(x) != len(y) {
		return false
	}

	exact := true
	for i := range x {
		if x[i] != y[i] {
			exact = false
			break
		}
	}
	if exact {
		return true
	}

	m := make(map[netip.Prefix]bool)
	for _, v := range x {
		m[v] = true
	}
	for _, v := range y {
		if !m[v] {
			return false
		}
	}
	return true
}
