//go:build !linux

package rntun

// tapデバイスに対応していない場合
func (*Wrapper) handleTAPFrame([]byte) bool { panic("unreachable") }
