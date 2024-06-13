// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package key

import (
	"crypto/subtle"
	"encoding/hex"

	"github.com/runetale/runetale/types/structs"
	"go4.org/mem"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	clientPrivateKeyPrefix = "private_client_key:"
	clientPublicKeyPrefix  = "public_client_key:"
)

type NodePrivateKey struct {
	_          structs.Incomparable
	privateKey wgtypes.Key
}

func NewClientPrivateKey() (NodePrivateKey, error) {
	k, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return NodePrivateKey{}, err
	}

	return NodePrivateKey{
		privateKey: k,
	}, nil
}

func (k NodePrivateKey) MarshalText() ([]byte, error) {
	return toHex(k.privateKey[:], clientPrivateKeyPrefix), nil
}

func (k *NodePrivateKey) UnmarshalText(b []byte) error {
	return parseHex(k.privateKey[:], mem.B(b), mem.S(clientPrivateKeyPrefix))
}

func (k NodePrivateKey) PublicKey() string {
	pkey := k.privateKey.PublicKey().String()
	return pkey
}

func (k NodePrivateKey) PrivateKey() string {
	pkey := k.privateKey.String()
	return pkey
}

func (k NodePrivateKey) Equal(other NodePrivateKey) bool {
	return subtle.ConstantTimeCompare(k.privateKey[:], other.privateKey[:]) == 1
}

func (k NodePrivateKey) UntypedHexString() string {
	return hex.EncodeToString(k.privateKey[:])
}
