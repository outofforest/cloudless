package thttp

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.uber.org/zap"

	"github.com/outofforest/cloudless/pkg/tcontext"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

const gracefulShutdownTimeout = 5 * time.Second

// Server wraps an HTTP server.
type Server struct {
	cfg      Config
	listener net.Listener
	options  []Option
	locked   sync.WaitGroup
}

// Config is a configuration of http service.
type Config struct {
	// Handler is a request handler
	Handler http.Handler

	// GetCertificate if non-nil turns on TLS and returns certificate for request.
	GetCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)
}

// An Option is a server configuration mixin.
//
// It is a function that modifies a http.Server before it starts serving
// requests.
type Option func(*http.Server)

// NewServer creates a Server.
func NewServer(listener net.Listener, cfg Config, opt ...Option) *Server {
	return &Server{
		listener: listener,
		cfg:      cfg,
		options:  opt,
	}
}

// Run serves requests until the context is closed, then performs graceful
// shutdown for up to gracefulShutdownTimeout.
func (s *Server) Run(ctx context.Context) error {
	ctx = logger.With(ctx, zap.Stringer("httpServer", s.listener.Addr()))
	reqCtx, reqCancel := context.WithCancel(tcontext.Reopen(ctx)) // stays open longer than ctx

	logger := logger.Get(ctx)
	errorLog, err := zap.NewStdLogAt(logger, zap.WarnLevel)
	lo.Must0(err)

	server := http.Server{
		Handler:     s.cfg.Handler,
		ErrorLog:    errorLog,
		BaseContext: func(net.Listener) context.Context { return reqCtx },
		ConnContext: s.connContext,
	}
	for _, opt := range s.options {
		opt(&server)
	}
	server.Handler = s.lock(server.Handler) // install as outermost

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("serve", parallel.Fail, func(ctx context.Context) error {
			logger.Info("Serving requests")
			var err error
			if s.cfg.GetCertificate != nil {
				server.TLSConfig = &tls.Config{
					GetCertificate: s.cfg.GetCertificate,
				}
				err = server.ServeTLS(s.listener, "", "")
			} else {
				err = server.Serve(s.listener)
			}

			// http.Server predates contexts, so it has its own
			// error meaning "terminated successfully due to an
			// external request". Return the actual error from
			// Context in this case to avoid accidentally treating
			// successful shutdown as an error.
			if errors.Is(err, http.ErrServerClosed) && ctx.Err() != nil {
				return errors.WithStack(ctx.Err())
			}
			return errors.WithStack(err)
		})

		spawn("shutdownHandler", parallel.Fail, func(ctx context.Context) error {
			<-ctx.Done()
			logger.Info("Shutting down")

			shutdownCtx, cancel := context.WithTimeout(reqCtx, gracefulShutdownTimeout)
			defer cancel()
			defer reqCancel()
			defer server.Close() // always returns nil because the listener is already closed

			// Server.Shutdown may return http.ErrServerClosed if
			// the server is already down. It's not an error in this
			// case.
			err := server.Shutdown(shutdownCtx)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("Shutdown failed", zap.Error(err))
				return errors.WithStack(err)
			}

			reqCancel() // ask hijacked connections to terminate
			s.locked.Wait()

			logger.Info("Shutdown complete")
			return errors.WithStack(ctx.Err())
		})
		return nil
	})
}

// ListenAddr returns the local address of the server's listener.
func (s *Server) ListenAddr() net.Addr {
	return s.listener.Addr()
}

// Unlock implements locker.Locker.
func (s *Server) Unlock() {
	s.locked.Done()
}

func (s *Server) connContext(ctx context.Context, conn net.Conn) context.Context {
	return logger.With(ctx, zap.Stringer("remoteAddr", conn.RemoteAddr()))
}

// This mandatory Middleware ensures that any running handlers prevent the
// server from shutting down. This is normally taken care of by the standard
// library itself, except when connections are hijacked. The latter use case is
// important for WebSocket.
func (s *Server) lock(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.locked.Add(1)
		defer s.locked.Done()
		next.ServeHTTP(w, r)
	})
}

// Middleware is an Option that installs top-level middleware on the HTTP server.
// The first middleware listed will be the first one to see the request.
func Middleware(mw ...func(http.Handler) http.Handler) Option {
	return func(s *http.Server) {
		for i := len(mw) - 1; i >= 0; i-- {
			s.Handler = mw[i](s.Handler)
		}
	}
}

// StandardMiddleware is a composition of typically used middleware, in the
// recommended order:
//
// 1. Log (log before and after the request)
// 2. Recover (catch and log panic)
// 3. CORS (allow cross-origin requests).
func StandardMiddleware(next http.Handler) http.Handler {
	return Log(Recover(CORS(next)))
}
