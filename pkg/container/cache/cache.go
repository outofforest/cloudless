package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.uber.org/zap"

	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
	"github.com/outofforest/cloudless/pkg/retry"
	"github.com/outofforest/cloudless/pkg/thttp"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

const port = 81

var (
	manifestMediaTypes = map[string]struct{}{
		"application/vnd.oci.image.manifest.v1+json":           {},
		"application/vnd.docker.distribution.manifest.v2+json": {},
	}
	configMediaTypes = map[string]struct{}{
		"application/vnd.oci.image.config.v1+json":       {},
		"application/vnd.docker.container.image.v1+json": {},
	}
	layerMediaTypes = map[string]struct{}{
		"application/vnd.oci.image.layer.v1.tar+gzip":       {},
		"application/vnd.docker.image.rootfs.diff.tar.gzip": {},
	}

	removeCharacters = []string{":", "/", ".", "?", "&", "#", "%", "\\"}
)

// Service returns new yum repo service.
func Service(repoRoot string, images ...string) host.Configurator {
	return func(c *host.Configuration) error {
		if len(images) == 0 {
			return nil
		}

		c.AddFirewallRules(firewall.OpenV4TCPPort(port))
		c.StartServices(host.ServiceConfig{
			Name:   "containercache",
			OnExit: parallel.Continue,
			TaskFn: func(ctx context.Context) error {
				return run(ctx, repoRoot, images)
			},
		})
		return nil
	}
}

func run(ctx context.Context, repoRoot string, images []string) error {
	for _, imageTag := range images {
		repoURL, imageTag := resolveImageTag(imageTag)

		m, authToken, err := fetchManifest(ctx, repoRoot, repoURL, imageTag)
		if err != nil {
			return err
		}

		if _, exists := manifestMediaTypes[m.MediaType]; !exists {
			return errors.Errorf("unsupported media type %s for manifest", m.MediaType)
		}
		if _, exists := configMediaTypes[m.Config.MediaType]; !exists {
			return errors.Errorf("unsupported config media type %s for config", m.Config.MediaType)
		}

		tagPos := strings.Index(imageTag, "@")
		if tagPos < 0 {
			return errors.Errorf("no tag in image %q", imageTag)
		}
		image := imageTag[:tagPos]

		authToken, err = fetchBlob(ctx, repoRoot, repoURL, authToken, image, m.Config.Digest)
		if err != nil {
			return err
		}

		for _, layer := range m.Layers {
			if _, exists := layerMediaTypes[layer.MediaType]; !exists {
				return errors.Errorf("unsupported layer media type %s for layer", layer.MediaType)
			}

			authToken, err = fetchBlob(ctx, repoRoot, repoURL, authToken, image, layer.Digest)
			if err != nil {
				return err
			}
		}
	}

	l, err := net.ListenTCP("tcp", &net.TCPAddr{Port: port})
	if err != nil {
		return errors.WithStack(err)
	}
	defer l.Close()

	server := thttp.NewServer(l, thttp.Config{
		Handler: http.FileServer(http.Dir(repoRoot)),
	})
	return server.Run(ctx)
}

func fetchManifest(ctx context.Context, repoRoot, repoURL, imageTag string) (manifest, string, error) {
	tagPos := strings.Index(imageTag, "@")
	if tagPos < 0 {
		return manifest{}, "", errors.Errorf("invalid imageTag name %q", imageTag)
	}
	image := imageTag[:tagPos]
	tag := imageTag[tagPos+1:]

	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", repoURL, image, tag)
	manifestFile := filepath.Join(repoRoot, sanitizeURL(manifestURL))
	manifestTmpFile := manifestFile + ".tmp"

	logger.Get(ctx).Info("Fetching manifest", zap.String("url", manifestURL))

	if err := os.MkdirAll(filepath.Dir(manifestFile), 0o700); err != nil {
		return manifest{}, "", errors.WithStack(err)
	}

	f, err := os.OpenFile(manifestTmpFile, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return manifest{}, "", errors.WithStack(err)
	}
	defer f.Close()

	var authToken string
	err = retry.Do(ctx, retry.FixedConfig{RetryAfter: 5 * time.Second, MaxAttempts: 10}, func() error {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return errors.WithStack(err)
		}
		if err := f.Truncate(0); err != nil {
			return errors.WithStack(err)
		}

		req := lo.Must(http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil))
		for mime := range manifestMediaTypes {
			req.Header.Add("Accept", mime)
		}
		if authToken != "" {
			req.Header.Add("Authorization", "Bearer "+authToken)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return retry.Retriable(err)
		}
		defer resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusUnauthorized:
			var err error
			authToken, err = authorize(ctx, resp.Header.Get("www-authenticate"))
			if err != nil {
				return err
			}

			return retry.ImmediatelyRetriable(errors.Errorf("authorization required: %q", manifestURL))
		case http.StatusOK:
		default:
			return retry.Retriable(errors.Errorf("unexpected response status: %d, %q", resp.StatusCode, manifestURL))
		}

		var r io.Reader = resp.Body
		var hasher hash.Hash
		if strings.HasPrefix(tag, "sha256:") {
			hasher = sha256.New()
			r = io.TeeReader(r, hasher)
		}

		if _, err := io.Copy(f, r); err != nil {
			return retry.Retriable(errors.WithStack(err))
		}

		if hasher != nil {
			computedDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
			if computedDigest != tag {
				if err := os.Remove(manifestTmpFile); err != nil {
					return errors.WithStack(err)
				}
				return retry.Retriable(errors.Errorf("digest doesn't match, expected: %s, got: %s", tag,
					computedDigest))
			}
		}

		return nil
	})

	if err != nil {
		return manifest{}, "", err
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return manifest{}, "", errors.WithStack(err)
	}

	var m manifest
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return manifest{}, "", errors.WithStack(err)
	}

	if err := os.Rename(manifestTmpFile, manifestFile); err != nil {
		return manifest{}, "", errors.WithStack(err)
	}

	return m, authToken, nil
}

func fetchBlob(ctx context.Context, repoRoot, repoURL, authToken, image, digest string) (string, error) {
	blobURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", repoURL, image, digest)
	blobFile := filepath.Join(repoRoot, sanitizeURL(blobURL))
	blobTmpFile := blobFile + ".tmp"

	logger.Get(ctx).Info("Fetching blob", zap.String("url", blobURL))

	if err := os.MkdirAll(filepath.Dir(blobFile), 0o700); err != nil {
		return "", errors.WithStack(err)
	}

	f, err := os.OpenFile(blobTmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", errors.WithStack(err)
	}
	defer f.Close()

	err = retry.Do(ctx, retry.FixedConfig{RetryAfter: 5 * time.Second, MaxAttempts: 10}, func() error {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return errors.WithStack(err)
		}
		if err := f.Truncate(0); err != nil {
			return errors.WithStack(err)
		}

		req := lo.Must(http.NewRequestWithContext(ctx, http.MethodGet, blobURL, nil))
		req.Header.Add("Authorization", "Bearer "+authToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return retry.Retriable(err)
		}
		defer resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusUnauthorized:
			var err error
			authToken, err = authorize(ctx, resp.Header.Get("www-authenticate"))
			if err != nil {
				return err
			}

			return retry.Retriable(errors.Errorf("authorization required: %q", blobURL))
		case http.StatusOK:
		default:
			return retry.Retriable(errors.Errorf("unexpected response status: %d, %q", resp.StatusCode, blobURL))
		}

		hasher := sha256.New()
		r := io.TeeReader(resp.Body, hasher)
		if _, err := io.Copy(f, r); err != nil {
			return retry.Retriable(errors.WithStack(err))
		}

		computedDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		if computedDigest != digest {
			if err := os.Remove(blobTmpFile); err != nil {
				return errors.WithStack(err)
			}
			return retry.Retriable(errors.Errorf("digest doesn't match, expected: %s, got: %s", digest,
				computedDigest))
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	if err := os.Rename(blobTmpFile, blobFile); err != nil {
		return "", errors.WithStack(err)
	}

	return authToken, nil
}

func authorize(ctx context.Context, authSetup string) (string, error) {
	url, err := authURL(authSetup)
	if err != nil {
		return "", errors.WithStack(err)
	}

	logger.Get(ctx).Info("Authorizing", zap.String("url", url))

	var authToken string
	err = retry.Do(ctx, retry.FixedConfig{RetryAfter: 5 * time.Second, MaxAttempts: 10}, func() error {
		resp, err := http.DefaultClient.Do(lo.Must(http.NewRequestWithContext(ctx, http.MethodGet, url, nil)))
		if err != nil {
			return retry.Retriable(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return retry.Retriable(errors.Errorf("unexpected response status: %d, %q", resp.StatusCode, url))
		}

		data := struct {
			Token       string `json:"token"`
			AccessToken string `json:"access_token"` //nolint:tagliatelle
		}{}

		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return retry.Retriable(err)
		}
		if data.Token != "" {
			authToken = data.Token
			return nil
		}
		if data.AccessToken != "" {
			authToken = data.AccessToken
			return nil
		}
		return retry.Retriable(errors.New("no token in response"))
	})
	if err != nil {
		return "", err
	}
	return authToken, nil
}

func authURL(authSetup string) (string, error) {
	spacePos := strings.Index(authSetup, " ")
	if spacePos < 0 {
		return "", errors.New("invalid auth setup format")
	}

	authMethod := authSetup[:spacePos]
	if strings.ToLower(authMethod) != "bearer" {
		return "", errors.Errorf("unsupported auth method %q", authMethod)
	}

	var authURL string
	first := true
	for _, kv := range strings.Split(authSetup[spacePos+1:], ",") {
		parts := strings.Split(kv, "=")
		if len(parts) != 2 {
			return "", errors.Errorf("invalid key-value %q", kv)
		}

		parts[1] = strings.ReplaceAll(parts[1], `"`, "")
		if parts[0] == "realm" {
			authURL = parts[1] + authURL
		} else {
			if first {
				first = false
				authURL += "?"
			} else {
				authURL += "&"
			}
			authURL += parts[0] + "=" + parts[1]
		}
	}
	return authURL, nil
}

func resolveImageTag(imageTag string) (string, string) {
	const defaultRegistry = "registry-1.docker.io"

	switch strings.Count(imageTag, "/") {
	case 0:
		return defaultRegistry, "library/" + imageTag
	case 1:
		return defaultRegistry, imageTag
	default:
		slashPos := strings.Index(imageTag, "/")
		url := imageTag[:slashPos] //nolint:gocritic // It is guaranteed here that / is there.
		if url == "docker.io" {
			url = defaultRegistry
		}
		return url, imageTag[slashPos+1:]
	}
}

func sanitizeURL(url string) string {
	for _, ch := range removeCharacters {
		url = strings.ReplaceAll(url, ch, "_")
	}

	return url
}

type manifest struct {
	MediaType string `json:"mediaType"`
	Config    struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
	} `json:"layers"`
}
