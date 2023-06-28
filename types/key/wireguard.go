// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package key

import (
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func NewGenerateKey() (string, error) {
	key, err := wgtypes.GenerateKey()
	if err != nil {
		panic(err)
	}
	return key.String(), nil
}
