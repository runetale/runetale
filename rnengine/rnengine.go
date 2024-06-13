package rnengine

import (
	"github.com/runetale/runetale/rnengine/router"
	"github.com/runetale/runetale/rnengine/wgconfig"
)

type Engine interface {
	Reconfig(wgConfig *wgconfig.WgConfig, routerConfig *router.Config) error
	Close()

	// TODO:
	// GetFilter returns the current packet filter, if any.
	// GetFilter() *filter.Filter

	// SetFilter updates the packet filter.
	// SetFilter(*filter.Filter)

	// SetNetworkMap(*netmap.NetworkMap)

	// UpdateStatus(*ipnstate.StatusBuilder)
}
