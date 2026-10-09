package updatefeed

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSavedProxyOverridesEnvironmentAndCanChange(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-Proxy", "first") }))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-Proxy", "second") }))
	defer second.Close()
	configured, _ := url.Parse(first.URL)
	client := NewHTTPClientWithProxy(func() *url.URL { return configured })
	for _, want := range []string{"first", "second"} {
		if want == "second" {
			configured, _ = url.Parse(second.URL)
		}
		resp, err := client.Get("http://release-source.invalid/test")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.Header.Get("X-Proxy") != want {
			t.Fatalf("request used unexpected proxy: %s", resp.Header.Get("X-Proxy"))
		}
	}
}
