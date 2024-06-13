package semaphore

import "context"

type Semaphore struct {
	c chan struct{}
}

func NewSemaphore(n int) Semaphore {
	return Semaphore{c: make(chan struct{}, n)}
}

// リソースを獲得するまでブロックを獲得する。
func (s Semaphore) Acquire() {
	s.c <- struct{}{}
}

// TryAcquire は、リソースが取得されたかどうかをブロックして報告する。
func (s Semaphore) AcquireContext(ctx context.Context) bool {
	select {
	case s.c <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// TryAcquire は、リソースが取得されたかどうかをブロックせずに報告します。
func (s Semaphore) TryAcquire() bool {
	select {
	case s.c <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s Semaphore) Release() {
	<-s.c
}
