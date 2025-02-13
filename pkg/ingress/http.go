package ingress

import (
	"bufio"
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
	"time"

	"github.com/pkg/errors"
	"github.com/ridge/must"
	"go.uber.org/zap"

	"github.com/outofforest/cloudless/pkg/thttp"
	"github.com/outofforest/cloudless/pkg/tnet"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

const (
	certificateUnstableRefreshInterval = 10 * time.Second
	certificateStableRefreshInterval   = time.Minute
)

// New creates new http ingress.
func New(cfg Config) *HTTPIngress {
	return &HTTPIngress{
		cfg:       cfg,
		endpoints: map[EndpointID][]*endpoint{},
	}
}

// HTTPIngress is the http ingress.
type HTTPIngress struct {
	cfg       Config
	endpoints map[EndpointID][]*endpoint

	mu   sync.RWMutex
	cert *tls.Certificate
}

// Run runs the ingress servers.
func (i *HTTPIngress) Run(ctx context.Context) (retErr error) {
	bindings := map[string]*binding{}

	for eID, e := range i.cfg.Endpoints {
		if i.endpoints[eID] != nil {
			return errors.Errorf("duplicated http HTTPIngress endpoint: %s", eID)
		}
		var endpoints []*endpoint
		if e.HTTPSMode != HTTPSModeOnly {
			for _, b := range e.PlainBindings {
				if bindings[b] == nil {
					bindings[b] = newBinding(false)
				}
				if bindings[b].Secure {
					return errors.Errorf("binding %s is used in both secure and plain bindings", b)
				}
				endpoints = append(endpoints, bindings[b].addEndpoint(b, eID, e))
			}
		}
		if e.HTTPSMode != HTTPSModeDisabled {
			for _, b := range e.SecureBindings {
				if bindings[b] == nil {
					bindings[b] = newBinding(true)
				}
				if !bindings[b].Secure {
					return errors.Errorf("binding %s is used in both secure and plain bindings", b)
				}
				endpoints = append(endpoints, bindings[b].addEndpoint(b, eID, e))
			}
		}
		i.endpoints[eID] = endpoints
	}

	for eID, targets := range i.cfg.Targets {
		if err := i.registerTargets(eID, targets); err != nil {
			return err
		}
	}

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("certificate", parallel.Fail, func(ctx context.Context) error {
			log := logger.Get(ctx).With(zap.String("url", i.cfg.CertificateURL))

			ticker := time.NewTicker(certificateUnstableRefreshInterval)
			defer ticker.Stop()

		loop:
			for {
				select {
				case <-ctx.Done():
					return errors.WithStack(ctx.Err())
				case <-ticker.C:
					tlsCert, err := fetchCertificate(ctx, i.cfg.CertificateURL)
					if err != nil {
						log.Error("Fetching certificate failed", zap.Error(err))

						if i.cert != nil && time.Until(i.cert.Leaf.NotAfter) < 2*certificateStableRefreshInterval {
							ticker.Reset(certificateUnstableRefreshInterval)
						}

						continue loop
					}

					if i.cert == nil || tlsCert.Leaf.NotAfter.After(i.cert.Leaf.NotAfter) {
						i.setCertificate(tlsCert)
					}

					if time.Until(i.cert.Leaf.NotAfter) < 2*certificateStableRefreshInterval {
						ticker.Reset(certificateUnstableRefreshInterval)
					} else {
						ticker.Reset(certificateStableRefreshInterval)
					}
				}
			}
		})

		for bAddr, b := range bindings {
			cfg := thttp.Config{Handler: b.handler()}
			if b.Secure {
				cfg.GetCertificate = func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
					i.mu.RLock()
					defer i.mu.RUnlock()

					return i.cert, nil
				}
			}

			spawn("server", parallel.Fail, func(ctx context.Context) error {
				ctx = logger.With(ctx, zap.String("localAddr", bAddr))

				l, err := tnet.Listen(ctx, bAddr)
				if err != nil {
					return errors.WithStack(err)
				}
				defer l.Close()

				return thttp.NewServer(l, cfg, thttp.Middleware(thttp.StandardMiddleware)).Run(ctx)
			})
		}

		return nil
	})
}

func (i *HTTPIngress) registerTargets(endpointID EndpointID, targets []Target) error {
	if i.endpoints[endpointID] == nil {
		return errors.Errorf("endpoint %s does not exist", endpointID)
	}

	for _, t := range targets {
		if t.Path == "" || t.Path[len(t.Path)-1] != '/' {
			return errors.Errorf("path must end with /, got: %s", t.Path)
		}

		for _, e := range i.endpoints[endpointID] {
			e.registerTarget(t)
		}
	}
	return nil
}

func (i *HTTPIngress) setCertificate(cert *tls.Certificate) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.cert = cert
}

type endpoint struct {
	id             EndpointID
	address        string
	secure         bool
	cfg            Endpoint
	allowedMethods map[string]bool
	allowedDomains map[string]bool

	mu      sync.RWMutex
	targets []string
}

// ServeHTTP serves http traffic.
//
//nolint:gocyclo
func (e *endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !e.allowedMethods[r.Method] {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hostPort := strings.SplitN(r.Host, ":", 2)
	if len(e.allowedDomains) > 0 && !e.allowedDomains[hostPort[0]] {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	isWebsocket := r.Header.Get("Upgrade") == "websocket"
	if isWebsocket && (!e.cfg.AllowWebsockets || r.Method != http.MethodGet) {
		w.WriteHeader(http.StatusForbidden)
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
	if _, err := io.Copy(w, resp.Body); err != nil {
		http.Error(w, "Proxy Error", http.StatusInternalServerError)
		log.Error("Error on copying response", zap.Error(err))
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

func (e *endpoint) registerTarget(target Target) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.targets = append(e.targets, net.JoinHostPort(target.Host, strconv.FormatUint(uint64(target.Port), 10))+target.Path)
}

func (e *endpoint) randomTarget() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.targets) == 0 {
		return ""
	}

	return e.targets[rand.Intn(len(e.targets))]
}

func newBinding(secure bool) *binding {
	return &binding{
		mux:    http.NewServeMux(),
		Secure: secure,
	}
}

type binding struct {
	mux *http.ServeMux

	Secure bool
}

func (b *binding) handler() http.Handler {
	return b.mux
}

func (b *binding) addEndpoint(address string, id EndpointID, cfg Endpoint) *endpoint {
	e := newEndpoint(address, b.Secure, id, cfg)
	if len(cfg.AllowedDomains) > 0 {
		for _, domain := range cfg.AllowedDomains {
			b.mux.Handle(domain+cfg.Path, e)
		}
	} else {
		b.mux.Handle(cfg.Path, e)
	}
	return e
}

var skipHeaders = map[string]bool{
	"X-Request-Id": true,
	"Location":     true,
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		if skipHeaders[k] {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func newEndpoint(address string, secure bool, id EndpointID, cfg Endpoint) *endpoint {
	allowedMethods := make(map[string]bool, len(cfg.AllowedMethods))
	for _, m := range cfg.AllowedMethods {
		allowedMethods[m] = true
	}
	allowedDomains := make(map[string]bool, len(cfg.AllowedDomains))
	for _, d := range cfg.AllowedDomains {
		allowedDomains[d] = true
	}
	return &endpoint{
		address:        address,
		secure:         secure,
		id:             id,
		cfg:            cfg,
		allowedMethods: allowedMethods,
		allowedDomains: allowedDomains,
	}
}

func fetchCertificate(ctx context.Context, url string) (*tls.Certificate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer resp.Body.Close()

	cert, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.WithStack(err)
	}

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

	tlsCert.Leaf, err = x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return tlsCert, nil
}
