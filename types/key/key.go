// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package key

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"go4.org/mem"
)

func fromHexChar(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}

	return 0, false
}

func toHex(k []byte, prefix string) []byte {
	ret := make([]byte, len(prefix)+len(k)*2)
	copy(ret, prefix)
	hex.Encode(ret[len(prefix):], k)
	return ret
}

func parseHex(out []byte, in, prefix mem.RO) error {
	if !mem.HasPrefix(in, prefix) {
		return fmt.Errorf("key hex string doesn't have expected type prefix %s", prefix.StringCopy())
	}
	in = in.SliceFrom(prefix.Len())
	if want := len(out) * 2; in.Len() != want {
		return fmt.Errorf("key hex has the wrong size, got %d want %d", in.Len(), want)
	}
	for i := range out {
		a, ok1 := fromHexChar(in.At(i*2 + 0))
		b, ok2 := fromHexChar(in.At(i*2 + 1))
		if !ok1 || !ok2 {
			return errors.New("invalid hex character in key")
		}
		out[i] = (a << 4) | b
	}

	return nil
}

type NodePublic struct {
	k [32]byte
}

func (k NodePublic) Less(other NodePublic) bool {
	return bytes.Compare(k.k[:], other.k[:]) < 0
}

func (k NodePublic) UntypedHexString() string {
	return hex.EncodeToString(k.k[:])
}

func ParseNodePrivateUntyped(raw mem.RO) (NodePrivateKey, error) {
	var ret NodePrivateKey
	if err := parseHex(ret.privateKey[:], raw, mem.B(nil)); err != nil {
		return NodePrivateKey{}, err
	}
	return ret, nil
}

func ParseNodePublicUntyped(raw mem.RO) (NodePublic, error) {
	var ret NodePublic
	if err := parseHex(ret.k[:], raw, mem.B(nil)); err != nil {
		return NodePublic{}, err
	}
	return ret, nil
}
