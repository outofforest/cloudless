package ingress

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"math/rand"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/ridge/must"
	"github.com/samber/lo"
	"go.uber.org/zap"

	"github.com/outofforest/cloudless/pkg/acme/wire"
	"github.com/outofforest/cloudless/pkg/thttp"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
	"github.com/outofforest/wave"
)

// New creates new http ingress.
func New(cfg Config) *HTTPIngress {
	return &HTTPIngress{
		cfg:       cfg,
		endpoints: map[EndpointID][]*endpoint{},
		certs:     map[string]*tls.Certificate{},
	}
}

// HTTPIngress is the http ingress.
type HTTPIngress struct {
	cfg       Config
	endpoints map[EndpointID][]*endpoint
	domains   []string

	mu    sync.RWMutex
	certs map[string]*tls.Certificate
}

// Run runs the ingress servers.
func (i *HTTPIngress) Run(ctx context.Context) (retErr error) {
	bindings := map[net.Listener]*binding{}

	var enableHttps bool
	allowedDomains := map[string]struct{}{}
	for eID, e := range i.cfg.Endpoints {
		if i.endpoints[eID] != nil {
			return errors.Errorf("duplicated http HTTPIngress endpoint: %s", eID)
		}

		for _, d := range e.AllowedDomains {
			allowedDomains[d] = struct{}{}
		}
		var endpoints []*endpoint
		if e.HTTPSMode != HTTPSModeOnly {
			for _, ls := range e.PlainListeners {
				if bindings[ls] == nil {
					bindings[ls] = newBinding(ls, false)
				}
				if bindings[ls].Secure {
					return errors.Errorf("address %s is used in both secure and plain bindings", ls.Addr())
				}
				endpoints = append(endpoints, bindings[ls].addEndpoint(eID, e))
			}
		}
		if e.HTTPSMode != HTTPSModeDisabled {
			enableHttps = true
			for _, ls := range e.TLSListeners {
				if bindings[ls] == nil {
					bindings[ls] = newBinding(ls, true)
				}
				if !bindings[ls].Secure {
					return errors.Errorf("address %s is used in both secure and plain bindings", ls.Addr())
				}
				endpoints = append(endpoints, bindings[ls].addEndpoint(eID, e))
			}
		}
		i.endpoints[eID] = endpoints
	}

	for eID, targets := range i.cfg.Targets {
		if err := i.registerTargets(eID, targets); err != nil {
			return err
		}
	}

	i.domains = lo.Keys(allowedDomains)

	m := wire.NewMarshaller()
	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		if enableHttps {
			waveClient, waveCh, err := wave.NewClient(wave.ClientConfig{
				Servers:        i.cfg.WaveServers,
				MaxMessageSize: 10 * 1024,
				Requests: []wave.RequestConfig{
					{
						Marshaller: m,
						Messages:   []any{&wire.MsgCertificate{}},
					},
				},
			})
			if err != nil {
				return err
			}

			spawn("wave", parallel.Fail, waveClient.Run)
			spawn("certificate", parallel.Fail, func(ctx context.Context) error {
				for {
					select {
					case <-ctx.Done():
						return errors.WithStack(ctx.Err())
					case msg := <-waveCh:
						certMsg, ok := msg.(*wire.MsgCertificate)
						if !ok {
							return errors.New("unexpected message type")
						}

						if err := i.setCertificate(certMsg.Certificate); err != nil {
							return err
						}
					}
				}
			})
		}
		for _, b := range bindings {
			cfg := thttp.Config{Handler: b.handler()}
			if b.Secure {
				cfg.GetCertificate = func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
					i.mu.RLock()
					defer i.mu.RUnlock()

					return i.certs[info.ServerName], nil
				}
			}

			spawn("server", parallel.Fail, func(ctx context.Context) error {
				return thttp.NewServer(b.ls, cfg, thttp.Middleware(thttp.StandardMiddleware)).Run(ctx)
			})
		}

		return nil
	})
}

func (i *HTTPIngress) registerTargets(endpointID EndpointID, targets []TargetConfig) error {
	if i.endpoints[endpointID] == nil {
		return errors.Errorf("endpoint %s does not exist", endpointID)
	}

	for _, t := range targets {
		if t.Path == "" || t.Path[len(t.Path)-1] != '/' {
			return errors.Errorf("path must end with /, got: %s", t.Path)
		}

		for _, e := range i.endpoints[endpointID] {
			e.targets = append(e.targets, net.JoinHostPort(t.Host, strconv.FormatUint(uint64(t.Port), 10))+t.Path)
		}
	}
	return nil
}

func (i *HTTPIngress) setCertificate(cert []byte) error {
	tlsCert, err := parseCertificate(cert)
	if err != nil {
		return err
	}

	supportedDomains := map[string]struct{}{}
	if tlsCert.Leaf.Subject.CommonName != "" {
		supportedDomains[tlsCert.Leaf.Subject.CommonName] = struct{}{}
	}
	for _, n := range tlsCert.Leaf.DNSNames {
		if n != "" {
			supportedDomains[n] = struct{}{}
		}
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	for _, d := range i.domains {
		if containsDomain(d, supportedDomains) {
			existingCert, exists := i.certs[d]
			if !exists || tlsCert.Leaf.NotAfter.After(existingCert.Leaf.NotAfter) {
				i.certs[d] = tlsCert
			}
		}
	}

	return nil
}

func containsDomain(domain string, domains map[string]struct{}) bool {
	if _, exists := domains[domain]; exists {
		return true
	}
	for {
		pos := strings.Index(domain, ".")
		if pos < 0 {
			return false
		}
		domain = domain[pos+1:]
		if _, exists := domains["*."+domain]; exists {
			return true
		}
	}
}

type cacheItem struct {
	ContentType string
	Content     []byte
}

type endpoint struct {
	id                      EndpointID
	secure                  bool
	cfg                     EndpointConfig
	allowedMethods          map[string]struct{}
	allowedDomains          map[string]struct{}
	allowedOrigins          map[string]struct{}
	preflightAllowedMethods string
	targets                 []string
	cachedContentTypes      map[string]struct{}

	mu    sync.RWMutex
	cache map[string]*cacheItem
}

// ServeHTTP serves http traffic.
//
//nolint:gocyclo
func (e *endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// FIXME (wojciech): Support websockets over HTTP2:
	// if r.Method == http.MethodConnect && r.Header.Get(":protocol") == "websocket" {
	// Use https://github.com/coder/websocket library which handles both http 1.1 and 2.0

	if _, exists := e.allowedMethods[r.Method]; !exists {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hostPort := strings.SplitN(r.Host, ":", 2)
	if _, exists := e.allowedDomains[hostPort[0]]; !exists {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	isWebsocket := r.Header.Get("Upgrade") == "websocket"
	if isWebsocket && (!e.cfg.AllowWebsockets || r.Method != http.MethodGet) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		_, exists := e.allowedOrigins[origin]
		if !exists {
			_, exists = e.allowedOrigins["*"]
		}
		if !exists {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header()["Access-Control-Allow-Origin"] = []string{origin}
	}

	if r.Method == http.MethodOptions {
		if e.preflightAllowedMethods != "" {
			w.Header()["Access-Control-Allow-Methods"] = []string{e.preflightAllowedMethods}
		}
		w.Header()["Access-Control-Allow-Headers"] = []string{"Content-Type,Authorization"}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	isHTTPS := r.TLS != nil

	url := *r.URL
	url.Host = r.Host
	switch {
	case isWebsocket && isHTTPS:
		url.Scheme = "wss"
	case isWebsocket:
		url.Scheme = "ws"
	case isHTTPS:
		url.Scheme = "https"
	default:
		url.Scheme = "http"
	}

	var redirect bool
	if !isHTTPS && e.cfg.HTTPSMode == HTTPSModeRedirect {
		url.Scheme = "https"
		if isWebsocket {
			url.Scheme = "wss"
		}
		redirect = true
	}
	if e.cfg.RemoveWWWPrefix && strings.HasPrefix(url.Host, "www.") {
		url.Host = strings.TrimPrefix(url.Host, "www.")
		redirect = true
	}
	if e.cfg.AddSlashToDirs && url.Path != "" && url.Path[len(url.Path)-1] != '/' {
		pos := strings.LastIndex(url.Path, "/")
		segment := url.Path[pos+1:]
		if !strings.Contains(segment, ".") {
			url.Path += "/"
			redirect = true
		}
	}
	if redirect {
		http.Redirect(w, r, url.String(), http.StatusMovedPermanently)
		return
	}

	urlStr := url.String()
	if !isWebsocket && r.Method == http.MethodGet {
		e.mu.RLock()
		content := e.cache[urlStr]
		e.mu.RUnlock()

		if content != nil {
			w.Header().Set("Content-Type", content.ContentType)
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Length", strconv.Itoa(len(content.Content)))
			_, _ = w.Write(content.Content)
			return
		}
	}

	log := logger.Get(r.Context())

	target := e.randomTarget()
	if target == "" {
		http.Error(w, "Proxy Error", http.StatusInternalServerError)
		log.Error("No target for endpoint", zap.Any("endpoint", e.id))
		return
	}

	fragments := strings.SplitN(target, "/", 2)
	targetPath := "/"
	if len(fragments) == 2 {
		targetPath += fragments[1]
	}

	tURL := url
	tURL.Host = fragments[0]
	tURL.Path = targetPath + strings.TrimPrefix(url.Path, e.cfg.Path)
	tURL.Scheme = "http"

	body := r.Body
	if r.Method == http.MethodGet {
		body = nil
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, tURL.String(), body)
	if err != nil {
		http.Error(w, "Proxy Error", http.StatusInternalServerError)
		log.Error("Error on creating request", zap.Error(err))
		return
	}
	req.Host = r.Host
	req.Header = r.Header.Clone()
	req.Header.Set("X-Original-Url", url.String())
	if clientIP, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		req.Header.Set("X-Forwarded-For", clientIP)
	}

	var netDialer net.Dialer
	targetConn, err := netDialer.DialContext(r.Context(), "tcp", tURL.Host)
	if err != nil {
		http.Error(w, "Proxy Error", http.StatusInternalServerError)
		log.Error("Error on connecting to target", zap.Error(err))
		return
	}
	defer targetConn.Close()

	if err := req.Write(targetConn); err != nil {
		http.Error(w, "Proxy Error", http.StatusInternalServerError)
		log.Error("Error on sending request to target", zap.Error(err))
		return
	}

	br := bufio.NewReader(targetConn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		http.Error(w, "Proxy Error", http.StatusInternalServerError)
		log.Error("Error on reading response from target", zap.Error(err))
		return
	}
	if resp == nil {
		return
	}
	defer resp.Body.Close()

	var compressedBuf *bytes.Buffer
	var gzipWriter *gzip.Writer
	contentType := resp.Header.Get("Content-Type")
	var respReader io.Reader = resp.Body
	if !isWebsocket && r.Method == http.MethodGet && contentType != "" {
		ct := contentType
		if i := strings.Index(ct, ";"); i > 0 {
			ct = ct[:i]
		}
		if _, exists := e.cachedContentTypes[ct]; exists {
			compressedBuf = bytes.NewBuffer(nil)
			gzipWriter = gzip.NewWriter(compressedBuf)
			defer gzipWriter.Close()
			respReader = io.TeeReader(respReader, gzipWriter)
		}
	}

	copyHeader(w.Header(), resp.Header)
	if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound {
		newLocation := resp.Header.Get("Location")
		switch {
		case newLocation == "":
			http.Error(w, "Proxy Error", http.StatusInternalServerError)
			log.Error("Requested redirection but Location is empty")
			return
		case must.Bool(regexp.MatchString("^[a-z]{1,6}://", newLocation)):
		case newLocation[0] != '/':
			newLocation = e.cfg.Path + newLocation
		case strings.HasPrefix(newLocation, targetPath):
			newLocation = e.cfg.Path + strings.TrimPrefix(newLocation, targetPath)
		}
		w.Header()["Location"] = []string{newLocation}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := respReader.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
			flusher.Flush()
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return
		}
	}

	if gzipWriter != nil {
		if err := gzipWriter.Flush(); err != nil {
			return
		}
		e.mu.Lock()
		e.cache[urlStr] = &cacheItem{
			ContentType: contentType,
			Content:     compressedBuf.Bytes(),
		}
		e.mu.Unlock()
		return
	}

	//nolint:nestif
	if resp.StatusCode == http.StatusSwitchingProtocols {
		h, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "Proxy Error", http.StatusInternalServerError)
			log.Error("Hijacking websocket connection is not possible")
			return
		}

		clientConn, _, err := h.Hijack()
		if err != nil {
			http.Error(w, "Proxy Error", http.StatusInternalServerError)
			log.Error("Hijacking client connection failed", zap.Error(err))
			return
		}
		defer clientConn.Close()

		err = parallel.Run(r.Context(), func(ctx context.Context, spawn parallel.SpawnFn) error {
			spawn("c2t", parallel.Exit, func(ctx context.Context) error {
				_, err = io.Copy(targetConn, clientConn)
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			})
			spawn("t2c", parallel.Exit, func(ctx context.Context) error {
				_, err = io.Copy(clientConn, targetConn)
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			})
			spawn("watchDog", parallel.Fail, func(ctx context.Context) error {
				<-ctx.Done()
				_ = clientConn.Close()
				_ = targetConn.Close()
				return ctx.Err()
			})

			return nil
		})

		if err != nil && !errors.Is(err, r.Context().Err()) {
			log.Error("Error on proxying request", zap.Error(err))
		}
	}
}

func (e *endpoint) randomTarget() string {
	if len(e.targets) == 0 {
		return ""
	}

	return e.targets[rand.Intn(len(e.targets))]
}

func newBinding(ls net.Listener, secure bool) *binding {
	return &binding{
		mux:    http.NewServeMux(),
		ls:     ls,
		Secure: secure,
	}
}

type binding struct {
	mux    *http.ServeMux
	ls     net.Listener
	Secure bool
}

func (b *binding) handler() http.Handler {
	return b.mux
}

func (b *binding) addEndpoint(id EndpointID, cfg EndpointConfig) *endpoint {
	e := newEndpoint(b.Secure, id, cfg)
	var h http.Handler = e
	if cfg.MaxBodyLength > 0 {
		h = http.MaxBytesHandler(h, cfg.MaxBodyLength)
	}
	if len(cfg.AllowedDomains) > 0 {
		for _, domain := range cfg.AllowedDomains {
			b.mux.Handle(domain+cfg.Path, h)
		}
	} else {
		b.mux.Handle(cfg.Path, h)
	}
	return e
}

var skipHeaders = map[string]struct{}{
	"X-Request-Id":                 {},
	"Location":                     {},
	"Access-Control-Allow-Origin":  {},
	"Access-Control-Allow-Methods": {},
	"Access-Control-Allow-Headers": {},
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		if _, exists := skipHeaders[k]; exists {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func newEndpoint(secure bool, id EndpointID, cfg EndpointConfig) *endpoint {
	allowedMethods := make(map[string]struct{}, len(cfg.AllowedMethods))
	for _, m := range cfg.AllowedMethods {
		allowedMethods[m] = struct{}{}
	}
	allowedDomains := make(map[string]struct{}, len(cfg.AllowedDomains))
	for _, d := range cfg.AllowedDomains {
		allowedDomains[d] = struct{}{}
	}
	allowedOrigins := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, d := range cfg.AllowedOrigins {
		allowedOrigins[d] = struct{}{}
	}

	cachedContentTypes := make(map[string]struct{}, len(cfg.CachedContentTypes))
	for _, ct := range cfg.CachedContentTypes {
		cachedContentTypes[ct] = struct{}{}
	}

	return &endpoint{
		secure:         secure,
		id:             id,
		cfg:            cfg,
		allowedMethods: allowedMethods,
		allowedDomains: allowedDomains,
		allowedOrigins: allowedOrigins,
		preflightAllowedMethods: strings.Join(lo.Filter(lo.Keys(allowedMethods), func(method string, i int) bool {
			return method != http.MethodOptions
		}), ","),
		cachedContentTypes: cachedContentTypes,
		cache:              map[string]*cacheItem{},
	}
}

func parseCertificate(cert []byte) (*tls.Certificate, error) {
	tlsCert := &tls.Certificate{}
	for {
		var block *pem.Block
		block, cert = pem.Decode(cert)
		if block == nil {
			break
		}

		switch block.Type {
		case "EC PRIVATE KEY":
			var err error
			tlsCert.PrivateKey, err = x509.ParseECPrivateKey(block.Bytes)
			if err != nil {
				return nil, errors.WithStack(err)
			}
		case "CERTIFICATE":
			tlsCert.Certificate = append(tlsCert.Certificate, block.Bytes)
		}
	}

	if tlsCert.PrivateKey == nil {
		return nil, errors.New("private key not present")
	}
	if len(tlsCert.Certificate) == 0 {
		return nil, errors.New("certificate chain not present")
	}

	var err error
	tlsCert.Leaf, err = x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return tlsCert, nil
}
