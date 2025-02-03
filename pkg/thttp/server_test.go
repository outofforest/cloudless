package thttp

import (
	"io"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/outofforest/cloudless/pkg/test"
	"github.com/outofforest/cloudless/pkg/tnet"
	"github.com/outofforest/parallel"
)

func TestServer(t *testing.T) {
	ctx := test.Context(t)
	group := parallel.NewGroup(ctx)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("hello"))
		assert.NoError(t, err)
	})

	s := NewServer(lo.Must(tnet.ListenOnRandomPort(ctx, tnet.NetworkTCP)), Config{Handler: handler},
		Middleware(StandardMiddleware))
	group.Spawn("server", parallel.Fail, s.Run)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.ListenAddr().String(), nil)
	require.NoError(t, err)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	assert.Equal(t, http.StatusOK, res.StatusCode)
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), body)
}
