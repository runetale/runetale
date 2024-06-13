// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package webrtc

import (
	"github.com/pion/stun/v2"
)

type StunTurnConfig struct {
	Stun *stun.URI
	Turn *stun.URI
}

func NewStunTurnConfig(
	stun *stun.URI,
	turn *stun.URI,
) *StunTurnConfig {
	return &StunTurnConfig{
		Stun: stun,
		Turn: turn,
	}
}

func (s *StunTurnConfig) GetStunTurnsURL() []*stun.URI {
	var urls []*stun.URI
	urls = append(urls, s.Stun)
	urls = append(urls, s.Turn)
	return urls
}
