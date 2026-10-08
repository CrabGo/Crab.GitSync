package updatefeed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

type testHost struct{}

func (*testHost) Emit(string, ...any) bool         { return false }
func (*testHost) OnEvent(string, func(any)) func() { return func() {} }
func (*testHost) OpenWindow(updater.WindowOptions) updater.WindowHandle {
	panic("unexpected update window")
}
func (*testHost) Quit() { panic("tests must never restart the application") }

func TestPrivateReleaseDownloadAndIntegrity(t *testing.T) {
	for _, test := range []struct {
		name    string
		corrupt bool
	}{{"valid", false}, {"corrupt", true}} {
		t.Run(test.name, func(t *testing.T) {
			stageRoot := t.TempDir()
			t.Setenv("TEMP", stageRoot)
			t.Setenv("TMP", stageRoot)
			t.Setenv("TMPDIR", stageRoot)
			content := []byte("test executable payload")
			hash := sha256.Sum256(content)
			digest := hex.EncodeToString(hash[:])
			cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("credential leaked to download CDN")
				}
				if test.corrupt {
					io.WriteString(w, "tampered")
				} else {
					w.Write(content)
				}
			}))
			defer cdn.Close()
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					w.WriteHeader(401)
					return
				}
				base := "/repos/CrabGo/Crab.GitSync"
				switch r.URL.Path {
				case base:
					io.WriteString(w, `{"private":true}`)
				case base + "/releases/latest", base + "/releases/tags/v0.3.0":
					json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.3.0", "name": "v0.3.0", "body": "release notes", "assets": []map[string]any{
						{"id": 1, "name": "crab-gitsync-windows-amd64.exe", "size": len(content), "url": server.URL + base + "/releases/assets/1", "browser_download_url": server.URL + "/private-browser-url", "digest": "sha256:" + digest},
						{"id": 2, "name": "SHA256SUMS", "url": server.URL + base + "/releases/assets/2"},
					}})
				case base + "/releases/assets/2":
					if r.Header.Get("Accept") != "application/octet-stream" {
						t.Error("checksum request not octet-stream")
					}
					fmt.Fprintf(w, "%s  crab-gitsync-windows-amd64.exe\n", digest)
				case base + "/releases/assets/1":
					http.Redirect(w, r, cdn.URL, http.StatusFound)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			provider, err := NewGitHub("CrabGo/Crab.GitSync", "test-token", server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			engine := updater.New(&testHost{})
			if err := engine.Init(updater.Config{CurrentVersion: "0.2.0", Platform: "windows", Arch: "amd64", Providers: []updater.Provider{provider}, Window: updater.WindowNone}); err != nil {
				t.Fatal(err)
			}
			release, err := engine.Check(context.Background())
			if err != nil || release == nil {
				t.Fatalf("check %v %v", release, err)
			}
			if release.Version != "0.3.0" || release.Verification == nil {
				t.Fatalf("release: %+v", release)
			}
			err = engine.DownloadAndInstall(context.Background())
			if test.corrupt {
				if err == nil || engine.State() != updater.StateError || engine.DownloadedPath() != "" {
					t.Fatal("corrupt payload accepted")
				}
			} else {
				if err != nil || engine.State() != updater.StateReady {
					t.Fatalf("stage: %v %v", err, engine.State())
				}
				bytes, err := os.ReadFile(engine.DownloadedPath())
				if err != nil || string(bytes) != string(content) {
					t.Fatal("incorrect staged executable")
				}
			}
		})
	}
}

func TestAccessDenialIsNotUpToDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer server.Close()
	provider, err := NewGitHub("CrabGo/Crab.GitSync", "token", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Check(context.Background(), updater.CheckRequest{CurrentVersion: "0.2.0", Platform: "windows", Arch: "amd64"}); err == nil {
		t.Fatal("inaccessible repository reported up-to-date")
	}
	if _, err := NewGitHub("CrabGo/Crab.GitSync", "", "", nil); err == nil {
		t.Fatal("missing authentication accepted")
	}
}

func TestMissingChecksumRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "Crab.GitSync") {
			io.WriteString(w, `{}`)
			return
		}
		io.WriteString(w, `{"tag_name":"v0.3.0","assets":[{"id":1,"name":"crab-gitsync-windows-amd64.exe","size":10,"browser_download_url":"https://github.com/a/b.exe"}]}`)
	}))
	defer server.Close()
	provider, _ := NewGitHub("CrabGo/Crab.GitSync", "token", server.URL, server.Client())
	if _, err := provider.Check(context.Background(), updater.CheckRequest{CurrentVersion: "0.2.0", Platform: "windows", Arch: "amd64"}); err == nil {
		t.Fatal("unverified release accepted")
	}
}

func TestMatchingAndTokenPriority(t *testing.T) {
	assets := []github.ReleaseAsset{{Name: "SHA256SUMS"}, {Name: "crab-gitsync-windows-arm64.exe"}, {Name: "crab-gitsync-windows-amd64.exe"}, {Name: "crab-gitsync-windows-amd64-installer.exe"}}
	if MatchAsset(updater.CheckRequest{Platform: "windows", Arch: "amd64"}, assets) != 2 {
		t.Fatal("wrong asset selected")
	}
	if MatchAsset(updater.CheckRequest{Platform: "darwin", Arch: "amd64"}, assets) != -1 {
		t.Fatal("other platform matched")
	}
	t.Setenv("GITSYNC_GITHUB_TOKEN", "private-value")
	t.Setenv("GH_TOKEN", "other-value")
	token, source := ResolveToken(context.Background())
	if token != "private-value" || source != "环境变量 GITSYNC_GITHUB_TOKEN" {
		t.Fatal("token priority failed")
	}
}
