package tws

import (
	"context"
	"net/http"
	"testing"

	"github.com/pkg/errors"
	"github.com/ridge/must"
	"github.com/stretchr/testify/require"

	"github.com/outofforest/cloudless/pkg/test"
	"github.com/outofforest/cloudless/pkg/thttp"
	"github.com/outofforest/cloudless/pkg/tnet"
	"github.com/outofforest/parallel"
)

func testPair(t *testing.T, server, client SessionFn) error {
	ctx := test.Context(t)

	l := must.NetListener(tnet.ListenOnRandomPort(ctx, tnet.NetworkTCP))
	httpServer := thttp.NewServer(l, thttp.Config{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Serve(w, r, DefaultConfig, server)
	})})

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("server", parallel.Fail, httpServer.Run)
		spawn("client", parallel.Exit, func(ctx context.Context) error {
			return Dial(ctx, "ws://"+l.Addr().String(), nil, DefaultConfig, client)
		})
		return nil
	})
}

func TestConnectionClosedByClient(t *testing.T) {
	require.NoError(t, testPair(t, func(ctx context.Context, incoming <-chan []byte, outgoing chan<- []byte) error {
		select {
		case <-ctx.Done():
			return errors.New("context closed too early")
		case _, ok := <-incoming:
			if ok {
				return errors.New("unexpected message received")
			}
			return nil
		}
	}, func(ctx context.Context, incoming <-chan []byte, outgoing chan<- []byte) error {
		return nil
	}))
}

func TestConnectionClosedByServer(t *testing.T) {
	require.NoError(t, testPair(t, func(ctx context.Context, incoming <-chan []byte, outgoing chan<- []byte) error {
		return nil
	}, func(ctx context.Context, incoming <-chan []byte, outgoing chan<- []byte) error {
		select {
		case <-ctx.Done():
			return errors.New("context closed too early")
		case _, ok := <-incoming:
			if ok {
				return errors.New("unexpected message received")
			}
			return nil
		}
	}))
}

func TestCommunication(t *testing.T) {
	require.NoError(t, testPair(t, func(ctx context.Context, incoming <-chan []byte, outgoing chan<- []byte) error {
		outgoing <- []byte("a")
		test.AssertEvents(ctx, t, incoming, []byte("b"))
		outgoing <- []byte("c")
		test.AssertEvents(ctx, t, incoming, []byte("d"))
		outgoing <- []byte("e")
		<-incoming
		return errors.WithStack(ctx.Err())
	}, func(ctx context.Context, incoming <-chan []byte, outgoing chan<- []byte) error {
		test.AssertEvents(ctx, t, incoming, []byte("a"))
		outgoing <- []byte("b")
		test.AssertEvents(ctx, t, incoming, []byte("c"))
		outgoing <- []byte("d")
		test.AssertEvents(ctx, t, incoming, []byte("e"))
		select {
		case outgoing <- []byte("f"):
		case <-ctx.Done():
		}
		return errors.WithStack(ctx.Err())
	}))
}
