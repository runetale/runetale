// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package store

import (
	"fmt"
	"log"
	"sync"

	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/types/key"
)

type ClientManager interface {
	GetPrivateKey() string
	GetPublicKey() string
}

type ClientStore struct {
	storeManager FileStoreManager
	privateKey   key.RunetaleClientPrivateState
	runelog      *runelog.Runelog

	mu sync.Mutex
}

// client Store initialization method.
func NewClientStore(f FileStoreManager, runelog *runelog.Runelog) *ClientStore {
	return &ClientStore{
		storeManager: f,
		runelog:      runelog,

		mu: sync.Mutex{},
	}
}

// read the PrivateKey from the Client State, and if it does not exist, write a new one.
func (c *ClientStore) WritePrivateKey() error {
	stateKey, err := c.storeManager.ReadState(ClientPrivateKeyStateKey)
	if err == nil {
		if err := c.privateKey.UnmarshalText(stateKey); err != nil {
			return fmt.Errorf("unable to unmarshal %s. %v", ClientPrivateKeyStateKey, err)
		}
		return nil
	}

	// create new client private key
	k, err := key.NewClientPrivateKey()
	if err != nil {
		return err
	}

	ke, err := k.MarshalText()
	if err != nil {
		log.Fatal(err)
		return err
	}

	// write new client private key
	if err := c.storeManager.WriteState(ClientPrivateKeyStateKey, ke); err != nil {
		c.runelog.Logger.Errorf("error writing client private key to store: %v.", err)
		return err
	}

	c.privateKey = k
	c.runelog.Logger.Debugf("write new client private key")

	return nil
}

func (c *ClientStore) GetPublicKey() string {
	stateKey, err := c.storeManager.ReadState(ClientPrivateKeyStateKey)
	if err == nil {
		if err := c.privateKey.UnmarshalText(stateKey); err != nil {
			c.runelog.Logger.Errorf("cannot marshal privatekey, %s", err.Error())
			// TODO: (shinta) need to be some supported
			return ""
		}
	}
	return c.privateKey.PublicKey()
}

func (c *ClientStore) GetPrivateKey() string {
	stateKey, err := c.storeManager.ReadState(ClientPrivateKeyStateKey)
	if err == nil {
		if err := c.privateKey.UnmarshalText(stateKey); err != nil {
			c.runelog.Logger.Errorf("cannot marshal privatekey, %s", err.Error())
			// TODO: (shinta) need to be some supported
			return ""
		}
	}
	return c.privateKey.PrivateKey()
}
