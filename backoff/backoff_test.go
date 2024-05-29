package backoff_test

import (
	"errors"
	"os"
	"testing"

	"github.com/runetale/runetale/backoff"
)

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}

func Test_RetryBackoff(t *testing.T) {
	err := testRetry()
	if err == nil {
		t.Fatal("failed to retry backoff test")
	}
	t.Log(err)
}

func testRetry() error {
	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
	operation := func() error {
		err := errors.New("retry")
		if err != nil {
			return err
		}

		err = errors.New("permanent error")
		return backoff.Permanent(err)
	}

	if err := backoff.Retry(operation, b); err != nil {
		return err
	}

	return nil
}
