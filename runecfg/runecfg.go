package runecfg

import (
	"net/netip"
)

// minor changes
// 2024/07/23 (TCP, UDP)
type Node struct {
	// このNodeのIP Address
	Addresses []netip.Prefix

	//

}

// grpcからこの形で返ってくる
type FilterRule struct {
	// defaultのcidrは/32
	// srcのIPAddresses
	SrcIps []string

	// 使用する、プロトコルの一覧
	IPProto []uint

	// ACL通りに許可された、IPとPort
	Dsts []netip.Prefix
}
