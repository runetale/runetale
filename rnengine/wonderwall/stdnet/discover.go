package stdnet

import "github.com/pion/transport/v3"

type ExternalIFaceDiscover interface {
	IFaces() (string, error)
}

type iFaceDiscover interface {
	iFaces() ([]*transport.Interface, error)
}
