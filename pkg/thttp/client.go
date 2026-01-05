package thttp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"unicode"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/outofforest/logger"
)

// LoggingTransport is HTTP transport with logging to context logger.
type LoggingTransport struct {
	//nolint:containedctx // it's fine
	Context   context.Context
	Transport http.RoundTripper
}

// WithRequestsLogging returns an http client with logging from the context logger.
func WithRequestsLogging(ctx context.Context, client *http.Client) *http.Client {
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &http.Client{
		Transport:     &LoggingTransport{ctx, transport},
		CheckRedirect: checkRedirect,
	}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > 10 {
		return errors.Errorf("request was terminated after 10 redirects")
	}
	// Go's http client removes Authorization from following request
	// https://github.com/golang/go/issues/35104
	for k, v := range via[0].Header {
		if _, exists := req.Header[k]; !exists {
			req.Header[k] = v
		}
	}
	return nil
}

// From https://stackoverflow.com/questions/53069040/checking-a-string-contains-only-ascii-characters.
func isASCII(s string) bool {
	for i := range s {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// RoundTrip is an implementation of RoundTripper
//
// RoundTripper is an interface representing the ability to execute a
// single HTTP transaction, obtaining the Response for a given Request.
//
// A RoundTripper must be safe for concurrent use by multiple
// goroutines.
func (t *LoggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	log := logger.Get(t.Context).With(zap.Stringer("url", req.URL), zap.String("method", req.Method))

	log.Info("HTTP request started")
	req.Body = t.logBody(log, "request", req.Body)

	resp, err := t.Transport.RoundTrip(req)
	if err != nil {
		log.Debug("HTTP request failed", zap.Error(err))
		return resp, err
	}

	resp.Body = t.logBody(log, "response", resp.Body)
	log.Info("HTTP request ended", zap.String("status", resp.Status))

	return resp, err
}

func (t *LoggingTransport) logBody(log *zap.Logger, subj string, body io.ReadCloser) io.ReadCloser {
	//nolint:nestif
	if body != nil {
		if ce := log.Check(zapcore.DebugLevel, "HTTP "+subj); ce != nil {
			data, err := io.ReadAll(body)
			if err != nil {
				logger.Get(t.Context).Debug("failed to read "+subj, zap.Error(err))
			}
			body = io.NopCloser(bytes.NewReader(data))

			if dataLen := len(data); dataLen > 0 {
				fields := []zap.Field{zap.Int(subj+"Length", dataLen)}
				if isASCII(string(data)) {
					fields = append(fields, zap.ByteString(subj+"Data", data))
				}
				ce.Write(fields...)
			}
		}
	}
	return body
}

// Test processes an http.Request (usually obtained from httptest.NewRequest)
// with the given handler as if it was received on the network. Only useful in
// tests.
//
// Does not require a running HTTP server to be running.
func Test(handler http.Handler, r *http.Request) *http.Response {
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w.Result()
}

// TestCtx is similar to Test, except that the given context is injected into
// the request.
func TestCtx(ctx context.Context, handler http.Handler, r *http.Request) *http.Response {
	return Test(handler, r.WithContext(ctx))
}
