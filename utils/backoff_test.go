package utils_test

import (
	"errors"
	"os"
	"testing"

	"github.com/runetale/runetale/utils"
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
	b := utils.WithMaxRetries(utils.NewExponentialBackOff(), 3)
	operation := func() error {
		err := errors.New("retry succeded")
		if err != nil {
			return err
		}

		return utils.Permanent(err)
	}

	if err := utils.Retry(operation, b); err != nil {
		return err
	}

	return nil
}
