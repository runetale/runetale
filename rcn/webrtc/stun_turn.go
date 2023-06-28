// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package webrtc

import "github.com/pion/ice/v2"

type StunTurnConfig struct {
	Stun *ice.URL
	Turn *ice.URL
}

func NewStunTurnConfig(
	stun *ice.URL,
	turn *ice.URL,
) *StunTurnConfig {
	return &StunTurnConfig{
		Stun: stun,
		Turn: turn,
	}
}

func (s *StunTurnConfig) GetStunTurnsURL() []*ice.URL {
	var urls []*ice.URL
	urls = append(urls, s.Stun)
	urls = append(urls, s.Turn)
	return urls
}
