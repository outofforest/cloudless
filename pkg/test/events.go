package test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func newReceiver(ch any) *receiver {
	chVal := reflect.ValueOf(ch)
	if chVal.Kind() != reflect.Chan {
		panic("ch is not a channel")
	}
	if chVal.Type().ChanDir()&reflect.RecvDir == 0 {
		panic("values can't be received from ch")
	}
	return &receiver{ch: chVal}
}

type receiver struct {
	ch reflect.Value
}

func (r *receiver) Receive(ctx context.Context) (any, bool, error) {
	chosen, recv, recvOK := reflect.Select([]reflect.SelectCase{
		{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())},
		{Dir: reflect.SelectRecv, Chan: r.ch},
	})
	if chosen == 0 {
		return nil, false, errors.WithStack(ctx.Err())
	}
	if !recvOK {
		return nil, false, nil
	}
	return recv.Interface(), true, nil
}

func (r *receiver) Cap() int {
	return r.ch.Cap()
}

func (r *receiver) Len() int {
	return r.ch.Len()
}

// AssertForefrontEvents asserts that the expected list of events was received on the actual channel.
func AssertForefrontEvents(ctx context.Context, t *testing.T, actualCh any, expected ...any) bool {
	r := newReceiver(actualCh)

	ok := true
	for i, e := range expected {
		res := func() bool {
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()

			val, valOK, err := r.Receive(ctx)
			//nolint:testifylint
			if !assert.NoErrorf(t, err, "timeout, index: %d", i) {
				return false
			}
			if !assert.Truef(t, valOK, "channel closed, index: %d", i) {
				return false
			}
			ok = ok && assert.Equal(t, e, val)
			return true
		}()
		if !res {
			return false
		}
	}
	return ok
}

// AssertEvents asserts that the expected list of events was received on the actual channel and no unexpected events
// are enqueued there.
func AssertEvents(ctx context.Context, t *testing.T, actualCh any, expected ...any) bool {
	if !AssertForefrontEvents(ctx, t, actualCh, expected...) {
		return false
	}

	r := newReceiver(actualCh)

	ok := true
	for i := 0; i < r.Cap() && r.Len() > 0; i++ {
		val, valOK, _ := r.Receive(ctx)
		if !valOK {
			break
		}
		assert.Fail(t, "unexpected event", "%#v", val)
		ok = false
	}

	return ok
}
