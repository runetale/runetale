package rntun

import (
	"os"

	"github.com/runetale/runetale/log"
)

func init() {
	tunDiagnoseFailure = diagnoseDarwinTUNFailure
}

func diagnoseDarwinTUNFailure(tunName string, log *log.Logger, err error) {
	if os.Getuid() != 0 {
		log.Logger.Debugf("failed to create TUN device as non-root user")
	}
	if tunName != "utun" {
		log.Logger.Debugf("failed to create TUN device %q", tunName)
	}
}
