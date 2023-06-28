// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package daemon

var SystemConfig = `[Unit]
Description=runetale daemon
Requires=NetworkManager.service
After=network-online.target
Wants=network-online.target systemd-networkd-wait-online.service

[Service]
User=root
Type=simple
ExecStart=/usr/bin/runetale up
Restart=on-failure
RestartSec=15s

[Install]
WantedBy=multi-user.target
`

const DaemonFilePath = "/etc/systemd/system/runetale.service"
const BinPath = "/usr/bin/runetale"
const ServiceName = "runetale"
