package gitengine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGitFetchUsesApplicationProxy(t *testing.T) {
	requests := make(chan string, 10)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests <- r.URL.String(); w.WriteHeader(502) }))
	defer proxy.Close()
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{{"init"}, {"remote", "add", "origin", "https://github.com/test/proxy-fixture.git"}, {"config", "http.proxy", "http://127.0.0.1:1"}, {"config", "http.https://github.com.proxy", "http://127.0.0.1:1"}} {
		if _, err := Run(ctx, dir, 5*time.Second, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := Fetch(WithProxy(ctx, proxy.URL), dir); err == nil {
		t.Fatal("expected test proxy's 502 response")
	}
	select {
	case target := <-requests:
		if target != "//github.com:443" && target != "github.com:443" {
			t.Fatalf("unexpected proxy target %s", target)
		}
	default:
		t.Fatal("Git did not use the configured proxy")
	}
	value, err := Run(ctx, dir, 5*time.Second, "config", "--get", "http.proxy")
	if err != nil || value != "http://127.0.0.1:1" {
		t.Fatal("application changed persisted repository Git settings")
	}
}
