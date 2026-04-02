package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"COM3D2TranslateTool/internal/model"
)

func newHTTPClient(proxy model.ProxyConfig, timeoutSeconds int, fallback time.Duration) (*http.Client, error) {
	timeout := fallback
	if timeoutSeconds > 0 {
		timeout = time.Duration(timeoutSeconds) * time.Second
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxyFunc, err := resolveProxyFunc(proxy)
	if err != nil {
		return nil, err
	}
	transport.Proxy = proxyFunc

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, nil
}

func resolveProxyFunc(proxy model.ProxyConfig) (func(*http.Request) (*url.URL, error), error) {
	switch normalizeProxyMode(proxy.Mode) {
	case "direct":
		return nil, nil
	case "custom":
		if strings.TrimSpace(proxy.URL) == "" {
			return nil, fmt.Errorf("custom proxy url is required")
		}
		parsed, err := parseProxyAddress(proxy.URL, "http")
		if err != nil {
			return nil, fmt.Errorf("invalid proxy url: %w", err)
		}
		return http.ProxyURL(parsed), nil
	default:
		return systemProxyFunc(), nil
	}
}

func normalizeProxyMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "direct":
		return "direct"
	case "custom":
		return "custom"
	default:
		return "system"
	}
}

func parseProxyAddress(rawServer string, fallbackScheme string) (*url.URL, error) {
	server := strings.TrimSpace(rawServer)
	if server == "" {
		return nil, fmt.Errorf("proxy url is empty")
	}

	if !strings.Contains(server, "://") {
		scheme := strings.TrimSpace(strings.ToLower(fallbackScheme))
		switch scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			scheme = "http"
		}
		server = scheme + "://" + server
	}

	parsed, err := url.Parse(server)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(parsed.Scheme) == "" {
		return nil, fmt.Errorf("proxy url is missing scheme")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return nil, fmt.Errorf("proxy url is missing host")
	}
	return parsed, nil
}

func doJSONRequest(ctx context.Context, client *http.Client, method string, endpoint string, headers map[string]string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	return doBytesRequest(ctx, client, method, endpoint, "application/json", headers, payload, out)
}

func doBytesRequest(ctx context.Context, client *http.Client, method string, endpoint string, contentType string, headers map[string]string, payload []byte, out any) error {
	if payload == nil {
		payload = []byte{}
	}

	const maxAttempts = 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		if strings.TrimSpace(contentType) != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := client.Do(req)
		if err != nil {
			if attempt < maxAttempts && isRetryableTransportError(err) {
				if sleepErr := waitRetryBackoff(ctx, attempt); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			return err
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			httpErr := readHTTPError(resp)
			_ = resp.Body.Close()
			if attempt < maxAttempts && isRetryableHTTPError(httpErr) {
				if sleepErr := waitRetryBackoff(ctx, attempt); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			return httpErr
		}

		if out == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return nil
		}

		decodeErr := json.NewDecoder(resp.Body).Decode(out)
		_ = resp.Body.Close()
		if decodeErr != nil {
			if attempt < maxAttempts && isRetryableTransportError(decodeErr) {
				if sleepErr := waitRetryBackoff(ctx, attempt); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			return decodeErr
		}
		return nil
	}

	return fmt.Errorf("request failed after retries")
}

type httpStatusError struct {
	StatusCode int
	Body       string
}

func (e *httpStatusError) Error() string {
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Sprintf("http %d", e.StatusCode)
	}
	return fmt.Sprintf("http %d: %s", e.StatusCode, e.Body)
}

func readHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	trimmed := string(bytes.TrimSpace(body))
	return &httpStatusError{
		StatusCode: resp.StatusCode,
		Body:       trimmed,
	}
}

func isRetryableHTTPError(err error) bool {
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return false
	}

	switch statusErr.StatusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout,
		520, 521, 522, 523, 524, 525, 526:
		return true
	default:
		return false
	}
}

func isRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unexpected eof") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "server closed idle connection") ||
		strings.Contains(message, "client connection lost")
}

func waitRetryBackoff(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt*attempt) * 500 * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
