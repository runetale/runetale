package rntun

import (
	"errors"
	"io"
	"os"

	"golang.zx2c4.com/wireguard/tun"
)

type kagemusha struct {
	evchan    chan tun.Event
	closechan chan struct{}
}

func NewKagemusha() tun.Device {
	return &kagemusha{
		evchan:    make(chan tun.Event),
		closechan: make(chan struct{}),
	}
}

func (t *kagemusha) File() *os.File {
	panic("kagemusha.File() called, which makes no sense")
}

func (t *kagemusha) Close() error {
	close(t.closechan)
	close(t.evchan)
	return nil
}

func (t *kagemusha) Read(out [][]byte, sizes []int, offset int) (int, error) {
	<-t.closechan
	return 0, io.EOF
}

func (t *kagemusha) Write(b [][]byte, n int) (int, error) {
	select {
	case <-t.closechan:
		return 0, errors.New("close kagemusha")
	default:
	}
	return 1, nil
}

const kagemushaName = "kagemusha"

func (t *kagemusha) Flush() error             { return nil }
func (t *kagemusha) MTU() (int, error)        { return 1500, nil }
func (t *kagemusha) Name() (string, error)    { return kagemushaName, nil }
func (t *kagemusha) Events() <-chan tun.Event { return t.evchan }
func (t *kagemusha) BatchSize() int           { return 1 }
func (t *kagemusha) Iskagemusha() bool        { return true }
