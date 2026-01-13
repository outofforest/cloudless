package wait

import (
	"context"
	"net/http"
	"time"

	"github.com/pkg/errors"

	"github.com/outofforest/logger"
)

// HTTP waits until http service starts responding.
func HTTP(ctx context.Context, addrs ...string) error {
	log := logger.Get(ctx)
	log.Info("Waiting for any host to be ready")
	for {
		var ok bool
		for _, m := range addrs {
			var err error
			ok, err = testAddr(ctx, m)
			if err != nil {
				if ctx.Err() != nil {
					return errors.WithStack(ctx.Err())
				}
				return err
			}
			if ok {
				break
			}
		}
		if ok {
			break
		}

		log.Info("No host ready yet, waiting before retrying...")

		select {
		case <-ctx.Done():
			return errors.WithStack(ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}

	return nil
}

func testAddr(ctx context.Context, url string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, errors.WithStack(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, nil
	}
	_ = resp.Body.Close()
	return true, nil
}
