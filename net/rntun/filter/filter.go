package filter

type Response int

// todo: fix
const (
	Drop         Response = iota // do not continue processing packet.
	DropSilently                 // do not continue processing packet, but also don't log
	Accept                       // continue processing packet.
	noVerdict                    // no verdict yet, continue running filter
)
