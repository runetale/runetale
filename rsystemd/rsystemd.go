package rsystemd

import (
	"fmt"
	"reflect"

	"github.com/runetale/runetale/net/rntun"
	"github.com/runetale/runetale/net/runedial"
	"github.com/runetale/runetale/rnengine"
	"github.com/runetale/runetale/rnengine/router"
	"github.com/runetale/runetale/rnengine/wonderwall"
)

type SubSystem[T any] struct {
	isSet bool
	v     T
}

type System struct {
	Dialer     SubSystem[*runedial.Dialer]
	Router     SubSystem[router.Router]
	Engine     SubSystem[rnengine.Engine]
	WonderConn SubSystem[*wonderwall.Conn]
	Tun        SubSystem[*rntun.Wrapper]

	onlyNetstack bool
}

func (s *System) Set(v any) {
	switch v := v.(type) {
	case *runedial.Dialer:
		s.Dialer.Set(v)
	case router.Router:
		s.Router.Set(v)
	case rnengine.Engine:
		s.Engine.Set(v)
	case *wonderwall.Conn:
		s.WonderConn.Set(v)
	case *rntun.Wrapper:
		type ft interface {
			IsFakeTun() bool
		}
		if _, ok := v.Unwrap().(ft); ok {
			s.onlyNetstack = true
		}
		s.Tun.Set(v)
	default:
		panic(fmt.Sprintf("unknown type %T", v))
	}
}

func (s *SubSystem[T]) Set(v T) {
	if s.isSet {
		var oldVal any = s.v
		var newVal any = v
		if oldVal == newVal {
			return
		}

		var z *T
		panic(fmt.Sprintf("%v is already set", reflect.TypeOf(z).Elem().String()))
	}
	s.v = v
	s.isSet = true
}

func (s *SubSystem[T]) Get() T {
	if !s.isSet {
		var z *T
		panic(fmt.Sprintf("%v is not set", reflect.TypeOf(z).Elem().String()))
	}
	return s.v
}

func (s *SubSystem[T]) GetIsSet() (_ T, ok bool) {
	return s.v, s.isSet
}

func (s *System) IsNetstack() bool {
	return s.onlyNetstack
}
