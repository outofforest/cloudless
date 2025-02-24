package profile

import (
	"context"
	"net"
	"net/http"
	"net/http/pprof"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/thttp"
	"github.com/outofforest/parallel"
)

// Port is the port profiler listens on.
const Port = 8089

// Service starts http server exposing pprof data.
func Service() host.Configurator {
	return cloudless.Service("profile", parallel.Fail, func(ctx context.Context) error {
		l, err := net.ListenTCP("tcp", &net.TCPAddr{Port: Port})
		if err != nil {
			return errors.WithStack(err)
		}
		defer l.Close()

		handler := http.NewServeMux()
		handler.HandleFunc("/", pprof.Index)
		handler.HandleFunc("/cmdline", pprof.Cmdline)
		handler.HandleFunc("/profile", pprof.Profile)
		handler.HandleFunc("/symbol", pprof.Symbol)
		handler.HandleFunc("/trace", pprof.Trace)

		server := thttp.NewServer(l, thttp.Config{
			Handler: handler,
		})
		return server.Run(ctx)
	})
}
