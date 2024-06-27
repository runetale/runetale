package runesystemd

import (
	"fmt"
	"reflect"

	"github.com/runetale/runetale/net/dial"
	"github.com/runetale/runetale/rnengine/router"
)

type SubSystem[T any] struct {
	set bool
	v   T
}

type System struct {
	Dialer SubSystem[*dial.Dialer]
	Router SubSystem[*router.Router]
}

func (s *System) Set(v any) {
	switch v := v.(type) {
	case *dial.Dialer:
		s.Dialer.Set(v)
	case *router.Router:
		s.Router.Set(v)
	default:
		panic(fmt.Sprintf("unknown type %T", v))
	}
}

func (p *SubSystem[T]) Set(v T) {
	if p.set {
		var oldVal any = p.v
		var newVal any = v
		if oldVal == newVal {
			return
		}

		var z *T
		panic(fmt.Sprintf("%v is already set", reflect.TypeOf(z).Elem().String()))
	}
	p.v = v
	p.set = true
}
