package platform

import "runtime"

func IsMobile() bool {
	return runtime.GOOS == "android" || runtime.GOOS == "ios"
}

func OS() string {
	if runtime.GOOS == "darwin" {
		return "macOS"
	}
	if runtime.GOOS == "ios" {
		return "iOS"
	}
	return runtime.GOOS
}
