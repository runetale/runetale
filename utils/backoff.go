package utils

import (
	"context"
	"time"
)

// backoff params
type BackoffParam struct {
	Interval    time.Duration
	MaxInterval time.Duration
	ElapsedTime time.Duration

	Factor     float64
	Multiplier float64

	currentInterval time.Duration
	startedAt       time.Time
}

func NewBackoffParam() BackoffExec {
	return &BackoffParam{}
}

type BackoffExec interface {
	Now() time.Time
	Stop() time.Duration
	Reset()
	NextBackoff() time.Duration
}

func (bp *BackoffParam) Now() time.Time {
	return time.Now()
}

func (bp *BackoffParam) Stop() time.Duration {
	return -1
}

func (bp *BackoffParam) Reset() {
	bp.currentInterval = bp.Interval
	bp.startedAt = bp.Now()
}

func (bp *BackoffParam) NextBackoff() time.Duration {
	return 0
}

// Timer
type Timer interface {
	Start(duration time.Duration)
	Stop()
	Done() <-chan time.Time
}

type execTimer struct {
	timer *time.Timer
}

func (et *execTimer) C() <-chan time.Time {
	return et.timer.C
}

func (et *execTimer) Start(duration time.Duration) {
	if et.timer == nil {
		et.timer = time.NewTimer(duration)
	} else {
		et.timer.Reset(duration)
	}
}

func (et *execTimer) Done() {
	if et.timer != nil {
		et.timer.Stop()
	}
}

// standard backoff struct
type BackOff struct {
	operation func() error
	notify    func(error, time.Duration)
	exec      BackoffExec
	execTimer *execTimer
}

// initialize backoff process
func NewBackoff(op func() error, exec BackoffExec, no func(error, time.Duration)) *BackOff {
	return &BackOff{
		operation: op,
		notify:    no,
		exec:      exec,
	}
}

func (b *BackOff) Retry() error {
	// todo: fix
	// var err error
	// var next time.Duration

	defer func() {
		b.execTimer.timer.Stop()
	}()

	b.exec.Reset()

	return nil
}

type BackoffExecContext interface {
	BackoffExec
	Context() context.Context
}

func getContext(exec BackoffExec) context.Context {
	if cb, ok := exec.(BackoffExecContext); ok {
		return cb.Context()
	}

	// todo: fix
	return nil
}
