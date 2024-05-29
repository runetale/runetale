// note: (snt)
// the backoff process is available at 'https://github.com/cenkalti/backoff' for reference.
// not enough processing to include in the package, so it's lightweight.

package backoff

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

const Stop time.Duration = -1

type BackOff interface {
	NextBackOff() time.Duration

	// Reset to initial state.
	Reset()
}

func WithMaxRetries(b BackOff, max uint64) BackOff {
	return &backOffTries{delegate: b, maxTries: max}
}

type backOffTries struct {
	delegate BackOff
	maxTries uint64
	numTries uint64
}

func (b *backOffTries) NextBackOff() time.Duration {
	if b.maxTries == 0 {
		return Stop
	}
	if b.maxTries > 0 {
		if b.maxTries <= b.numTries {
			return Stop
		}
		b.numTries++
	}
	return b.delegate.NextBackOff()
}

func (b *backOffTries) Reset() {
	b.numTries = 0
	b.delegate.Reset()
}

// Exponetial
type Clock interface {
	Now() time.Time
}

type ExponentialBackOff struct {
	InitialInterval     time.Duration
	RandomizationFactor float64
	Multiplier          float64
	MaxInterval         time.Duration
	// After MaxElapsedTime the ExponentialBackOff returns Stop.
	// It never stops if MaxElapsedTime == 0.
	MaxElapsedTime time.Duration
	Stop           time.Duration
	Clock          Clock

	currentInterval time.Duration
	startTime       time.Time
}

const (
	DefaultInitialInterval     = 500 * time.Millisecond
	DefaultRandomizationFactor = 0.5
	DefaultMultiplier          = 1.5
	DefaultMaxInterval         = 60 * time.Second
	DefaultMaxElapsedTime      = 15 * time.Minute
)

type systemClock struct{}

func (t systemClock) Now() time.Time {
	return time.Now()
}

var SystemClock = systemClock{}

type ExponentialBackOffOpts func(*ExponentialBackOff)

func NewExponentialBackOff(opts ...ExponentialBackOffOpts) *ExponentialBackOff {
	b := &ExponentialBackOff{
		InitialInterval:     DefaultInitialInterval,
		RandomizationFactor: DefaultRandomizationFactor,
		Multiplier:          DefaultMultiplier,
		MaxInterval:         DefaultMaxInterval,
		MaxElapsedTime:      DefaultMaxElapsedTime,
		Stop:                Stop,
		Clock:               SystemClock,
	}
	for _, fn := range opts {
		fn(b)
	}
	b.Reset()
	return b
}

// GetElapsedTime returns the elapsed time since an ExponentialBackOff instance
// is created and is reset when Reset() is called.
//
// The elapsed time is computed using time.Now().UnixNano(). It is
// safe to call even while the backoff policy is used by a running
// ticker.
func (b *ExponentialBackOff) GetElapsedTime() time.Duration {
	return b.Clock.Now().Sub(b.startTime)
}

// Increments the current interval by multiplying it with the multiplier.
func (b *ExponentialBackOff) incrementCurrentInterval() {
	// Check for overflow, if overflow is detected set the current interval to the max interval.
	if float64(b.currentInterval) >= float64(b.MaxInterval)/b.Multiplier {
		b.currentInterval = b.MaxInterval
	} else {
		b.currentInterval = time.Duration(float64(b.currentInterval) * b.Multiplier)
	}
}

// Returns a random value from the following interval:
//
//	[currentInterval - randomizationFactor * currentInterval, currentInterval + randomizationFactor * currentInterval].
func getRandomValueFromInterval(randomizationFactor, random float64, currentInterval time.Duration) time.Duration {
	if randomizationFactor == 0 {
		return currentInterval // make sure no randomness is used when randomizationFactor is 0.
	}
	var delta = randomizationFactor * float64(currentInterval)
	var minInterval = float64(currentInterval) - delta
	var maxInterval = float64(currentInterval) + delta

	// Get a random value from the range [minInterval, maxInterval].
	// The formula used below has a +1 because if the minInterval is 1 and the maxInterval is 3 then
	// we want a 33% chance for selecting either 1, 2 or 3.
	return time.Duration(minInterval + (random * (maxInterval - minInterval + 1)))
}

func (b *ExponentialBackOff) NextBackOff() time.Duration {
	// Make sure we have not gone over the maximum elapsed time.
	elapsed := b.GetElapsedTime()
	next := getRandomValueFromInterval(b.RandomizationFactor, rand.Float64(), b.currentInterval)
	b.incrementCurrentInterval()
	if b.MaxElapsedTime != 0 && elapsed+next > b.MaxElapsedTime {
		return b.Stop
	}
	return next
}

func (b *ExponentialBackOff) Reset() {
	b.currentInterval = b.InitialInterval
	b.startTime = b.Clock.Now()
}

// Retry
type Timer interface {
	Start(duration time.Duration)
	Stop()
	C() <-chan time.Time
}

// defaultTimer implements Timer interface using time.Timer
type defaultTimer struct {
	timer *time.Timer
}

// C returns the timers channel which receives the current time when the timer fires.
func (t *defaultTimer) C() <-chan time.Time {
	return t.timer.C
}

// Start starts the timer to fire after the given duration
func (t *defaultTimer) Start(duration time.Duration) {
	if t.timer == nil {
		t.timer = time.NewTimer(duration)
	} else {
		t.timer.Reset(duration)
	}
}

// Stop is called when the timer is not used anymore and resources may be freed.
func (t *defaultTimer) Stop() {
	if t.timer != nil {
		t.timer.Stop()
	}
}

type BackOffContext interface { // nolint: golint
	BackOff
	Context() context.Context
}

func getContext(b BackOff) context.Context {
	if cb, ok := b.(BackOffContext); ok {
		return cb.Context()
	}
	if tb, ok := b.(*backOffTries); ok {
		return getContext(tb.delegate)
	}
	return context.Background()
}

// An OperationWithData is executing by RetryWithData() or RetryNotifyWithData().
// The operation will be retried using a backoff policy if it returns an error.
type OperationWithData[T any] func() (T, error)

// An Operation is executing by Retry() or RetryNotify().
// The operation will be retried using a backoff policy if it returns an error.
type Operation func() error

func (o Operation) withEmptyData() OperationWithData[struct{}] {
	return func() (struct{}, error) {
		return struct{}{}, o()
	}
}

// Notify is a notify-on-error function. It receives an operation error and
// backoff delay if the operation failed (with an error).
//
// NOTE that if the backoff policy stated to stop retrying,
// the notify function isn't called.
type Notify func(error, time.Duration)

// Retry the operation o until it does not return error or BackOff stops.
// o is guaranteed to be run at least once.
//
// If o returns a *PermanentError, the operation is not retried, and the
// wrapped error is returned.
//
// Retry sleeps the goroutine for the duration returned by BackOff after a
// failed operation returns.
func Retry(o Operation, b BackOff) error {
	return RetryNotify(o, b, nil)
}

// RetryWithData is like Retry but returns data in the response too.
func RetryWithData[T any](o OperationWithData[T], b BackOff) (T, error) {
	return RetryNotifyWithData(o, b, nil)
}

// RetryNotify calls notify function with the error and wait duration
// for each failed attempt before sleep.
func RetryNotify(operation Operation, b BackOff, notify Notify) error {
	return RetryNotifyWithTimer(operation, b, notify, nil)
}

// RetryNotifyWithData is like RetryNotify but returns data in the response too.
func RetryNotifyWithData[T any](operation OperationWithData[T], b BackOff, notify Notify) (T, error) {
	return doRetryNotify(operation, b, notify, nil)
}

// RetryNotifyWithTimer calls notify function with the error and wait duration using the given Timer
// for each failed attempt before sleep.
// A default timer that uses system timer is used when nil is passed.
func RetryNotifyWithTimer(operation Operation, b BackOff, notify Notify, t Timer) error {
	_, err := doRetryNotify(operation.withEmptyData(), b, notify, t)
	return err
}

// RetryNotifyWithTimerAndData is like RetryNotifyWithTimer but returns data in the response too.
func RetryNotifyWithTimerAndData[T any](operation OperationWithData[T], b BackOff, notify Notify, t Timer) (T, error) {
	return doRetryNotify(operation, b, notify, t)
}

func doRetryNotify[T any](operation OperationWithData[T], b BackOff, notify Notify, t Timer) (T, error) {
	var (
		err  error
		next time.Duration
		res  T
	)
	if t == nil {
		t = &defaultTimer{}
	}

	defer func() {
		t.Stop()
	}()

	ctx := getContext(b)

	b.Reset()
	for {
		res, err = operation()
		if err == nil {
			return res, nil
		}

		var permanent *PermanentError
		if errors.As(err, &permanent) {
			return res, permanent.Err
		}

		if next = b.NextBackOff(); next == Stop {
			if cerr := ctx.Err(); cerr != nil {
				return res, cerr
			}

			return res, err
		}

		if notify != nil {
			notify(err, next)
		}

		t.Start(next)

		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-t.C():
		}
	}
}

type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}

func (e *PermanentError) Unwrap() error {
	return e.Err
}

func (e *PermanentError) Is(target error) bool {
	_, ok := target.(*PermanentError)
	return ok
}

// Permanent wraps the given err in a *PermanentError.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{
		Err: err,
	}
}
