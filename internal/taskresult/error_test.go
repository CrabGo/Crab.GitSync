package taskresult

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		err      error
		category string
		retry    bool
	}{
		{context.Canceled, "cancelled", false}, {fmt.Errorf("wrapped: %w", context.DeadlineExceeded), "timeout", true},
		{&net.DNSError{Err: "no such host", Name: "github.com"}, "dns", true},
		{errors.New("proxyconnect tcp: connection refused"), "proxy", true},
		{errors.New("Authentication failed for remote"), "authentication", false},
		{errors.New("Permission denied (publickey)"), "authentication", false},
		{errors.New("SSL certificate problem: self-signed certificate"), "tls", false},
		{errors.New("repository not found"), "remote_not_found", false},
		{errors.New("Failed to connect to github.com:443: Could not connect to server"), "connection", true},
		{errors.New("工作区或暂存区有修改"), "git_state", false},
		{errors.New("fatal: unexpected object format"), "unknown", false},
		{errors.New("fatal: object named timeout is invalid"), "unknown", false},
		{errors.New("The requested URL returned error: 403"), "authentication", false},
		{HTTPStatus(503), "connection", true}, {HTTPStatus(403), "authentication", false},
	}
	for _, tc := range cases {
		f := Wrap(tc.err, "fetch")
		if f.Category != tc.category || f.Retryable != tc.retry {
			t.Fatalf("%v: %+v", tc.err, f)
		}
		if !errors.Is(f, tc.err) {
			t.Fatal("lost cause")
		}
	}
}
func TestRefreshFailureAndRedaction(t *testing.T) {
	if f := Wrap(Wrap(context.DeadlineExceeded, "git"), "refresh"); f.Category != "refresh" || f.Retryable {
		t.Fatal("wrapped refresh could repeat fetch")
	}
	err := errors.New("failed to connect https://alice:secret@example.com/path?token=private#fragment socks5h://bob:pass@localhost:1080")
	f := Wrap(err, "refresh")
	if f.Category != "refresh" || f.Retryable {
		t.Fatal("refresh would retry network")
	}
	for _, secret := range []string{"alice", "secret", "token=", "private", "fragment", "bob", "pass@"} {
		if strings.Contains(f.Detail, secret) {
			t.Fatalf("credential leaked: %s", f.Detail)
		}
	}
	wrapped := Wrap(fmt.Errorf("fetch wrapper: %w", Wrap(context.Canceled, "git")), "fetch")
	if wrapped.Category != "cancelled" || !errors.Is(wrapped, context.Canceled) {
		t.Fatal("wrapped cancellation lost")
	}
	if Wrap(nil, "fetch") != nil {
		t.Fatal("successful operation created error")
	}
}

func TestRedactionPreservesQuotedURLDelimiters(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`Get "https://alice:secret@github.com/o/r?token=private": connection refused`, `Get "https://github.com/o/r": connection refused`},
		{`fatal: unable to access 'https://alice:secret@github.com/o/r.git/': error`, `fatal: unable to access 'https://github.com/o/r.git/': error`},
	} {
		if got := Redact(tc.input); got != tc.want {
			t.Fatalf("redaction %q != %q", got, tc.want)
		}
	}
}
