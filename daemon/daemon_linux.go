// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package daemon

import (
	"log"
	"os"
)

func newDaemon(
	binPath, serviceName, daemonFilePath, systemConfig string,
	logger *log.Logger,
) Daemon {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return &systemDRecord{
			binPath:        binPath,
			serviceName:    serviceName,
			daemonFilePath: daemonFilePath,
			systemConfig:   systemConfig,

			logger: logger,
		}
	}
	if _, err := os.Stat("/sbin/initctl"); err == nil {
		return &upstartRecord{
			binPath:        binPath,
			serviceName:    serviceName,
			daemonFilePath: daemonFilePath,
			systemConfig:   systemConfig,

			logger: logger,
		}
	}

	return &systemVRecord{
		binPath:        binPath,
		serviceName:    serviceName,
		daemonFilePath: daemonFilePath,
		systemConfig:   systemConfig,

		logger: logger,
	}
}
