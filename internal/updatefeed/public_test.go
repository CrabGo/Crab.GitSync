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
	"time"
)

func TestPublicReleaseWithoutCredentials(t *testing.T) {
	payload := []byte("public executable")
	digest := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
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
		case base + "download/v0.4.0/CHANGELOG.md":
			io.WriteString(w, "# v0.4.0\n\n## 新增\n同步进度")
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
	if release.Notes != "# v0.4.0\n\n## 新增\n同步进度" || release.Metadata["release.url"] != server.URL+"/CrabGo/Crab.GitSync/releases/tag/v0.4.0" {
		t.Fatalf("notes are not pinned to the release: %+v", release)
	}
	if err := p.Download(context.Background(), release, &out, func(int64, int64) {}); err != nil || !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("download: %v", err)
	}
	if release, err := p.Check(context.Background(), updater.CheckRequest{CurrentVersion: "0.4.0", Platform: "windows", Arch: "amd64"}); err != nil || release != nil {
		t.Fatalf("up-to-date: %v %v", release, err)
	}
}

func TestOptionalPublicNotesNeverBypassVerification(t *testing.T) {
	for _, mode := range []string{"missing", "oversized", "invalid-utf8", "timeout", "literal-html"} {
		t.Run(mode, func(t *testing.T) {
			digest := sha256.Sum256([]byte("exe"))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				switch {
				case strings.HasSuffix(r.URL.Path, "/latest"):
					http.Redirect(w, r, "/o/r/releases/tag/v0.5.1", 302)
				case strings.HasSuffix(r.URL.Path, "/CHANGELOG.md"):
					if r.URL.Path != "/o/r/releases/download/v0.5.1/CHANGELOG.md" {
						t.Error("notes tag differs from exe")
					}
					switch mode {
					case "missing":
						w.WriteHeader(404)
					case "oversized":
						io.WriteString(w, strings.Repeat("x", 65537))
					case "invalid-utf8":
						w.Write([]byte{0xff})
					case "timeout":
						<-r.Context().Done()
					case "literal-html":
						io.WriteString(w, "<script>alert(1)</script>\n## 新增\n- 示例")
					}
				case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
					fmt.Fprintf(w, "%x  crab-gitsync-windows-amd64.exe", digest)
				default:
					io.WriteString(w, "exe")
				}
			}))
			defer server.Close()
			p := NewPublicGitHub("o/r", server.URL, server.Client())
			start := time.Now()
			release, err := p.Check(context.Background(), updater.CheckRequest{CurrentVersion: "0.5.0", Platform: "windows", Arch: "amd64"})
			if err != nil || release == nil || !bytes.Equal(release.Verification.Digest, digest[:]) {
				t.Fatalf("optional notes blocked verified update: %+v %v", release, err)
			}
			if mode == "literal-html" {
				if !strings.Contains(release.Notes, "<script>") {
					t.Fatal("expected literal text")
				}
			} else if release.Notes != "" {
				t.Fatal("invalid notes should fall back to release page")
			}
			if mode == "timeout" && time.Since(start) > 5*time.Second {
				t.Fatal("notes timeout was not bounded")
			}
		})
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
