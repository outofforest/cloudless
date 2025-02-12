package tws

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

// Config is the WebSocket configuration.
type Config struct {
	// Timeout for the WebSocket protocol upgrade
	HandshakeTimeout time.Duration

	// Disconnect when an outgoing packet is not acknowledged for this long.
	// 0 for kernel default.
	TCPTimeout time.Duration

	// Send pings this often. 0 to disable.
	PingInterval time.Duration

	// Disconnect if a pong doesn't arrive during PingInterval
	RequirePong bool
}

// DefaultConfig is the default Config value.
var DefaultConfig = Config{
	HandshakeTimeout: 5 * time.Second,

	TCPTimeout: 30 * time.Second,

	PingInterval: 30 * time.Second,
	RequirePong:  true,
}

// StreamerConfig is the recommended configuration for high-traffic protocols
// where participants cannot be expected to be responsive all the time.
var StreamerConfig = func() Config {
	config := DefaultConfig
	config.RequirePong = false
	return config
}()

// SessionFn is a function that implements a WebSocket interaction scenario.
//
// The function receives incoming messages through one channel and sends outgoing messages through another.
// Both the incoming channel and the context will be closed when the connection closes.
// Once the session function returns, the connection will be closed if it's still open.
// If the session function returns nil, the closure will be graceul: the incoming channel will be drained first.
// If the session function returns an error, the connection will be closed immediately.
type SessionFn func(ctx context.Context, incoming <-chan []byte, outgoing chan<- []byte) error

// Serve handles an HTTP request by upgrading the connection to WebSocket
// and executing the interaction scenario described by the session function.
//
// The context passed into the session function is a descendant of the request context.
func Serve(w http.ResponseWriter, r *http.Request, config Config, sessionFn SessionFn) {
	upgrader := websocket.Upgrader{
		HandshakeTimeout: config.HandshakeTimeout,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	// Copying w.Header gets us, in particular, X-Request-Id set by
	// thttp.Log.
	respHeaders := r.Header.Clone()
	respHeaders.Del("Sec-WebSocket-Extensions")
	ws, err := upgrader.Upgrade(w, r, respHeaders)
	if err != nil {
		logger.Get(r.Context()).Error("Failed to serve WebSocket connection", zap.Error(err))
		return
	}

	if err := TuneTCP(ws.UnderlyingConn(), config); err != nil {
		ws.Close()
		logger.Get(r.Context()).Error("Failed to serve WebSocket connection", zap.Error(err))
		return
	}

	_ = handleSession(r.Context(), ws, config, sessionFn)
}

// Dial connects to WebSocket server and executes the interaction scenario described by the session function.
func Dial(ctx context.Context, url string, headers http.Header, config Config, sessionFn SessionFn) error {
	dialer := websocket.Dialer{
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var netDialer net.Dialer
			conn, err := netDialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, errors.WithStack(err)
			}
			err = TuneTCP(conn, config)
			if err != nil {
				conn.Close()
				return nil, errors.WithStack(err)
			}
			return conn, nil
		},
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: config.HandshakeTimeout,
	}

	ws, resp, err := dialer.DialContext(ctx, url, headers)
	if err != nil {
		return errors.Wrapf(err, "failed to establish WebSocket connection to %s", url)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = resp.Body.Close()
		return errors.Errorf("failed to establish WebSocket connection to %s: handshake failed: %s", url, resp.Status)
	}

	ctx = logger.With(ctx, zap.String("url", url), zap.String("requestID", resp.Header.Get("X-Request-Id")))
	return handleSession(ctx, ws, config, sessionFn)
}

func handleSession(ctx context.Context, ws *websocket.Conn, config Config, sessionFn SessionFn) error {
	log := logger.Get(ctx)
	log.Info("WebSocket established")

	var pings int64 // difference between pings sent and pongs received
	incoming := make(chan []byte)
	outgoing := make(chan []byte)

	if config.RequirePong {
		ws.SetPongHandler(func(data string) error {
			atomic.AddInt64(&pings, -1)
			return nil
		})
	}

	err := parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("session", parallel.Continue, func(ctx context.Context) error {
			defer close(outgoing)
			return sessionFn(ctx, incoming, outgoing)
		})
		spawn("receiver", parallel.Continue, func(ctx context.Context) error {
			defer close(incoming)

			for {
				mt, buff, err := ws.ReadMessage()
				if err != nil {
					if ctx.Err() != nil {
						return errors.WithStack(ctx.Err())
					}
					var errClose *websocket.CloseError
					if errors.As(err, &errClose) {
						return nil
					}
					return errors.WithStack(err)
				}
				switch mt {
				case websocket.TextMessage:
					select {
					case incoming <- buff:
					case <-ctx.Done():
						return errors.WithStack(ctx.Err())
					}
				default:
					return errors.Errorf("text message expected")
				}
			}
		})
		spawn("sender", parallel.Exit, func(ctx context.Context) error {
			// We use websocket pings because Nginx closes the connection if no traffic passes it
			// despite configuring TCP keepalive.

			var ticks <-chan time.Time
			if config.PingInterval != 0 {
				ticker := time.NewTicker(config.PingInterval)
				defer ticker.Stop()
				ticks = ticker.C
			}
			for {
				// Keep in mind that gorilla websocket library does not support concurrent writes (WriteMessage, WriteControl...)
				// so sending real messages and pings have to happen in the same goroutine or be protected by mutex.
				// We have chosen the first solution here.
				select {
				case msg, ok := <-outgoing:
					if !ok {
						return nil
					}
					if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil {
						return errors.WithStack(err)
					}
				case <-ticks:
					if config.RequirePong && atomic.AddInt64(&pings, 1) > 1 { // we still haven't received the previous pong
						return errors.New("websocket ping timeout")
					}
					if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
						return errors.WithStack(err)
					}
				}
			}
		})
		spawn("closer", parallel.Exit, func(ctx context.Context) error {
			<-ctx.Done()
			if err := ws.Close(); err != nil {
				return errors.WithStack(err)
			}
			return errors.WithStack(ctx.Err())
		})

		return nil
	})

	if err == nil || errors.Is(err, ctx.Err()) {
		log.Info("WebSocket disconnected", zap.Error(err))
	} else {
		log.Warn("WebSocket disconnected", zap.Error(err))
	}
	return err
}

// WithWSScheme changes http to ws and https to wss.
func WithWSScheme(addr string) string {
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		panic("no scheme in address")
	}
	return strings.Replace(addr, "http", "ws", 1)
}
