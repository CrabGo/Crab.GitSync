package updatefeed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicReleaseWithoutCredentials(t *testing.T) {
	payload := []byte("public executable")
	digest := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("unexpected authentication")
		}
		base := "/CrabGo/Crab.GitSync/releases/"
		switch r.URL.Path {
		case base + "latest":
			http.Redirect(w, r, base+"tag/v0.4.0", 302)
		case base + "tag/v0.4.0":
			io.WriteString(w, "release page")
		case base + "download/v0.4.0/SHA256SUMS":
			fmt.Fprintf(w, "%x  crab-gitsync-windows-amd64.exe\n", digest)
		case base + "download/v0.4.0/crab-gitsync-windows-amd64.exe":
			w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
			if r.Method != "HEAD" {
				w.Write(payload)
			}
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := NewPublicGitHub("CrabGo/Crab.GitSync", server.URL, server.Client())
	release, err := p.Check(context.Background(), updater.CheckRequest{CurrentVersion: "0.3.0", Platform: "windows", Arch: "amd64"})
	if err != nil || release == nil || release.Version != "0.4.0" || !bytes.Equal(release.Verification.Digest, digest[:]) {
		t.Fatalf("check: %+v %v", release, err)
	}
	var out bytes.Buffer
	if err := p.Download(context.Background(), release, &out, func(int64, int64) {}); err != nil || !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("download: %v", err)
	}
	if release, err := p.Check(context.Background(), updater.CheckRequest{CurrentVersion: "0.4.0", Platform: "windows", Arch: "amd64"}); err != nil || release != nil {
		t.Fatalf("up-to-date: %v %v", release, err)
	}
}

func TestPublicReleaseRejectsMissingAssetsOrInvalidTags(t *testing.T) {
	for _, test := range []struct{ tag, checksum string }{{"v0.4.0", "not-a-digest"}, {"v0.4.0-beta.1", ""}, {"invalid", ""}} {
		t.Run(test.tag, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/latest") {
					http.Redirect(w, r, "/o/r/releases/tag/"+test.tag, 302)
				} else {
					io.WriteString(w, test.checksum)
				}
			}))
			defer server.Close()
			p := NewPublicGitHub("o/r", server.URL, server.Client())
			if _, err := p.Check(context.Background(), updater.CheckRequest{CurrentVersion: "0.3.0", Platform: "windows", Arch: "amd64"}); err == nil {
				t.Fatal("invalid release accepted")
			}
		})
	}
}
