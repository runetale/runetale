// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package conf

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/tun"
	"github.com/runetale/runetale/types/key"
	"github.com/runetale/runetale/utils"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// spec is a runetale config json
// path's here => '/etc/runetale/client.json'
type ClientConfig struct {
	WgPrivateKey string   `json:"wg_private_key"`
	ServerHost   string   `json:"server_host"`
	ServerPort   uint     `json:"server_port"`
	SignalHost   string   `json:"signal_host"`
	SignalPort   uint     `json:"signal_port"`
	TunName      string   `json:"tun"`
	PreSharedKey string   `json:"preshared_key"`
	BlackList    []string `json:"blacklist"`

	path    string
	isDebug bool

	log *log.Logger
}

func NewClientConfig(
	path string,
	serverHost string, serverPort uint,
	signalHost string, signalPort uint,
	isDebug bool,
	dl *log.Logger,
) (*ClientConfig, error) {
	return &ClientConfig{
		ServerHost: serverHost,
		ServerPort: serverPort,
		SignalHost: signalHost,
		SignalPort: signalPort,
		path:       path,
		isDebug:    isDebug,
		log:        dl,
	}, nil
}

func (c *ClientConfig) writeJson(
	wgPrivateKey, tunName string,
	serverHost string,
	serverPort uint,
	signalHost string,
	signalPort uint,
	blackList []string,
	presharedKey string,
) *ClientConfig {
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		c.log.Logger.Warnf("failed to create directory with %s, because %s", c.path, err.Error())
	}

	c.ServerHost = serverHost
	c.ServerPort = serverPort
	c.SignalHost = signalHost
	c.SignalPort = signalPort
	c.WgPrivateKey = wgPrivateKey
	c.TunName = tunName
	c.BlackList = blackList

	b, err := json.MarshalIndent(*c, "", "\t")
	if err != nil {
		panic(err)
	}

	if err = utils.AtomicWriteFile(c.path, b, 0755); err != nil {
		panic(err)
	}

	return c
}

func (c *ClientConfig) CreateClientConfig() *ClientConfig {
	defaultInterfaceBlacklist := []string{
		tun.TunName(), "wt", "utun", "tun0", "zt", "ZeroTier", "wg", "ts",
		"Tailscale", "tailscale", "docker", "veth", "br-", "lo",
	}

	b, err := os.ReadFile(c.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		privKey, err := key.NewGenerateKey()
		if err != nil {
			c.log.Logger.Error("failed to generate key for wireguard")
			panic(err)
		}

		return c.writeJson(
			privKey,
			tun.TunName(),
			c.ServerHost,
			c.ServerPort,
			c.SignalHost,
			c.SignalPort,
			defaultInterfaceBlacklist,
			"",
		)
	case err != nil:
		c.log.Logger.Errorf("%s could not be read. exception error: %s", c.path, err.Error())
		panic(err)
	default:
		var cc ClientConfig
		if err := json.Unmarshal(b, &cc); err != nil {
			c.log.Logger.Warnf("can not read client config file, because %v", err)
		}

		var serverhost string
		var signalhost string

		// todo (snt) refactor
		// for daemon
		if c.ServerHost == "" {
			serverhost = c.ServerHost
		} else {
			serverhost = c.ServerHost
		}

		if c.SignalHost == "" {
			signalhost = c.SignalHost
		} else {
			signalhost = c.SignalHost
		}

		return c.writeJson(
			cc.WgPrivateKey,
			cc.TunName,
			serverhost,
			c.ServerPort,
			signalhost,
			c.SignalPort,
			cc.BlackList,
			"",
		)
	}
}

func (c *ClientConfig) GetClientConfig() (*ClientConfig, error) {
	var cc ClientConfig
	b, err := os.ReadFile(c.path)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(b, &cc); err != nil {
		c.log.Logger.Warnf("can not read client config file, because %v", err)
		return nil, err
	}

	return &cc, nil
}

// return to host:port
func (c *ClientConfig) GetServerHost() string {
	return c.buildHost(c.ServerHost, c.ServerPort)
}

// return to host:port
func (c *ClientConfig) GetSignalHost() string {
	return c.buildHost(c.SignalHost, c.SignalPort)
}

func (c *ClientConfig) buildHost(host string, port uint) string {
	var h string
	var p string
	if !c.isDebug {
		h = strings.Replace(host, "https://", "", -1)
	} else {
		h = strings.Replace(host, "http://", "", -1)
	}

	p = strconv.Itoa(int(port))
	return h + ":" + p
}

func (c *ClientConfig) GetWgPublicKey() (string, error) {
	parsedKey, err := wgtypes.ParseKey(c.WgPrivateKey)
	if err != nil {
		return "", err
	}
	return parsedKey.PublicKey().String(), nil
}
