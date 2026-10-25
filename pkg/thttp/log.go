package thttp

import (
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/outofforest/cloudless/pkg/idgen"
	"github.com/outofforest/logger"
)

// Log is a middleware that logs before and after handling of each request.
// Does not include logging of request and response bodies.
//
// Each request is assigned a unique ID which is logged and sent to the client
// as X-Ridge-RequestID header.
func Log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = idgen.Random.ID()
			r.Header.Set("X-Request-ID", requestID)
		}
		ctx := logger.With(r.Context(),
			zap.String("requestID", requestID),
			zap.String("method", r.Method),
			zap.String("host", r.Host),
			zap.Stringer("url", r.URL),
		)
		log := logger.Get(ctx)
		log.Info("HTTP request handling started")
		var status int
		w.Header()["X-Request-Id"] = []string{requestID}
		next.ServeHTTP(CaptureStatus(w, &status), r.WithContext(ctx))
		log.Info("HTTP request handling ended", zap.Int("statusCode", status), zap.Duration("elapsed", time.Since(started)))
	})
}
