// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package store

import "errors"

type StateKey string

var ErrStateNotFound = errors.New("state not found")

const (
	ClientPrivateKeyStateKey = StateKey("client-private-key")
)
