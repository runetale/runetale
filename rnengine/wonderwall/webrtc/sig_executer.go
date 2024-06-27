// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package webrtc

// this package provides the functions needed for udp hole punching using webrtc
// dependent on signal client
//

import (
	"github.com/runetale/runetale/log"

	"github.com/pion/ice/v2"
	"github.com/runetale/runetale/client/grpc"
)

type SigExecuter struct {
	signalClient grpc.SignalClientImpl
	dstmk        string
	srcmk        string

	log *log.Logger
}

func NewSigExecuter(
	signalClient grpc.SignalClientImpl,
	dstmk string,
	srcmk string,
	logger *log.Logger,
) *SigExecuter {
	return &SigExecuter{
		signalClient: signalClient,
		dstmk:        dstmk,
		srcmk:        srcmk,

		log: logger,
	}
}

func (s *SigExecuter) Candidate(
	candidate ice.Candidate,
) {
	if candidate != nil {
		go func() {
			err := s.signalClient.Candidate(s.dstmk, s.srcmk, candidate)
			if err != nil {
				s.log.Logger.Errorf("failed to candidate against signal server, becasuse %s", err.Error())
				return
			}
		}()
	}
}

func (s *SigExecuter) Offer(
	uFlag string,
	pwd string,
) error {
	return s.signalClient.Offer(s.dstmk, s.srcmk, uFlag, pwd)
}

func (s *SigExecuter) Answer(
	uFlag string,
	pwd string,
) error {
	return s.signalClient.Answer(s.dstmk, s.srcmk, uFlag, pwd)
}
