package netease

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
)

// Automatic recovery used to retry missing, forbidden and malformed API results forever.
func TestRecoveryClassifiesGatewayFailures(t *testing.T) {
	old := httpClient
	t.Cleanup(func() { httpClient = old })
	for _, tc := range []struct {
		name   string
		status int
		body   string
		retry  bool
	}{
		{"missing", 404, `{}`, false},
		{"gone", 410, `{}`, false},
		{"forbidden", 403, `{}`, false},
		{"timeout", 408, `{}`, true},
		{"limited", 429, `{}`, true},
		{"service", 503, `{}`, true},
		{"API missing", 200, `{"code":404}`, false},
		{"API signed out", 200, `{"code":301}`, false},
		{"API risk check", 200, `{"code":-460}`, false},
		{"API busy", 200, `{"code":503}`, true},
		{"invalid body", 200, `<html>`, false},
		{"truncated JSON", 200, `{"code":`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jar, _ := cookiejar.New(nil)
			httpClient = &http.Client{Jar: jar, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Status: http.StatusText(tc.status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			for _, gateway := range []string{"web", "eapi"} {
				var out any
				var err error
				if gateway == "web" {
					req, _ := http.NewRequest("GET", "https://example.test/api/test", nil)
					err = do(req, &out)
				} else {
					err = eapiOK("/api/test", map[string]any{})
				}
				if err == nil || RetryableError(fmt.Errorf("wrapped: %w", err)) != tc.retry {
					t.Errorf("%s: retry=%v, err=%v", gateway, tc.retry, err)
				}
			}
		})
	}
	for _, err := range []error{ErrOffline, context.DeadlineExceeded, &net.DNSError{IsTimeout: true}, io.ErrUnexpectedEOF} {
		if !RetryableError(err) {
			t.Errorf("connection failure cannot recover: %v", err)
		}
	}
	if RetryableError(nil) || RetryableError(&url.Error{Op: "Get", Err: context.Canceled}) {
		t.Fatal("success or cancellation became an automatic retry")
	}
}

// URL wrappers must not turn invalid URLs or certificate refusals into transient network failures.
func TestRecoveryRejectsPermanentURLFailures(t *testing.T) {
	for _, err := range []error{
		&url.Error{Op: "Get", URL: "file:///cover", Err: fmt.Errorf("unsupported protocol scheme")},
		&url.Error{Op: "Get", URL: "https://example.test", Err: x509.UnknownAuthorityError{}},
	} {
		if RetryableError(err) {
			t.Errorf("permanent request error became retryable: %v", err)
		}
	}
	if !RetryableError(&url.Error{Op: "Get", Err: &net.DNSError{IsTimeout: true}}) {
		t.Fatal("URL timeout lost automatic recovery")
	}
}
