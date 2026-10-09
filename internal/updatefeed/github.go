// Package updatefeed adapts GitHub Releases for authenticated, verified updates.
package updatefeed

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// ResolveToken never exposes credentials in an error or the frontend state.
func ResolveToken(ctx context.Context) (token, source string) {
	for _, name := range []string{"GITSYNC_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value, "环境变量 " + name
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com")
	hideWindow(cmd)
	output, err := cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) != "" {
		return strings.TrimSpace(string(output)), "GitHub CLI"
	}
	return "", "未认证"
}

func NewHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxyForRequest()
	return &http.Client{Transport: transport, Timeout: 5 * time.Minute}
}

// NewHTTPClientWithProxy reads saved settings for each request so changing the
// proxy never requires reinitialising the Wails updater.
func NewHTTPClientWithProxy(proxy func() *url.URL) *http.Client {
	client := NewHTTPClient()
	transport := client.Transport.(*http.Transport)
	fallback := transport.Proxy
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		if configured := proxy(); configured != nil {
			return configured, nil
		}
		return fallback(req)
	}
	return client
}

type GitHubProvider struct {
	inner             updater.Provider
	client            *http.Client
	base, repo, token string
}

// NewGitHub uses exact asset names and API URLs so private release assets work too.
func NewGitHub(repo, token, base string, client *http.Client) (*GitHubProvider, error) {
	if token == "" {
		return nil, fmt.Errorf("私有发布源需要认证，请先运行 gh auth login，或设置 GITSYNC_GITHUB_TOKEN")
	}
	if base == "" {
		base = "https://api.github.com"
	}
	if client == nil {
		client = NewHTTPClient()
	}
	inner, err := github.New(github.Config{
		Repository: repo, Token: token, BaseURL: base, HTTPClient: client,
		AssetMatcher: MatchAsset,
	})
	if err != nil {
		return nil, err
	}
	return &GitHubProvider{inner: inner, client: client, base: strings.TrimRight(base, "/"), repo: repo, token: token}, nil
}

// MatchAsset excludes installers, checksum files and other platform binaries.
func MatchAsset(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	name := "crab-gitsync-" + req.Platform + "-" + req.Arch
	if req.Platform == "windows" {
		name += ".exe"
	}
	for i, asset := range assets {
		if asset.Name == name {
			return i
		}
	}
	return -1
}

func (p *GitHubProvider) Name() string { return "github" }

func (p *GitHubProvider) get(ctx context.Context, endpoint, accept string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	// Never forward a credential to the CDN behind a release-asset redirect.
	client := *p.client
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("更新下载重定向过多")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 GitHub 发布源，请检查网络或代理")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 {
			return nil, fmt.Errorf("GitHub 访问失败（HTTP %d），请检查令牌权限、仓库访问权限或 API 配额", resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub 发布源返回 HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (p *GitHubProvider) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	// Verify access separately: a 404 from /releases/latest can mean either no
	// published release or missing permission. Never call the latter up-to-date.
	body, err := p.get(ctx, p.base+"/repos/"+p.repo, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	body.Close()
	release, err := p.inner.Check(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("检查发布版本失败，请检查网络、认证以及发布文件是否匹配当前平台")
	}
	if release == nil {
		return nil, nil
	}
	tag, _ := release.Metadata["github.release.tag"].(string)
	body, err = p.get(ctx, p.base+"/repos/"+p.repo+"/releases/tags/"+tag, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var details struct {
		Assets []struct {
			Name   string `json:"name"`
			URL    string `json:"url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 2<<20)).Decode(&details); err != nil {
		return nil, fmt.Errorf("无法解析发布文件列表")
	}
	var assetURL, checksumURL, digest string
	for _, asset := range details.Assets {
		if asset.Name == release.Artifact.Filename {
			assetURL, digest = asset.URL, asset.Digest
		}
		if asset.Name == "SHA256SUMS" {
			checksumURL = asset.URL
		}
	}
	// Only authenticated GitHub API endpoints are allowed before redirects.
	assetPrefix := p.base + "/repos/" + p.repo + "/releases/assets/"
	if !strings.HasPrefix(assetURL, assetPrefix) || !strings.HasPrefix(checksumURL, assetPrefix) {
		return nil, fmt.Errorf("发布文件缺少程序或 SHA256SUMS 校验文件")
	}
	checksums, err := p.get(ctx, checksumURL, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(checksums, (1<<20)+1))
	checksums.Close()
	if err != nil || len(data) > 1<<20 {
		return nil, fmt.Errorf("无法读取发布校验文件")
	}
	var expected []byte
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == release.Artifact.Filename {
			expected, err = hex.DecodeString(fields[0])
			break
		}
	}
	if err != nil || len(expected) != 32 {
		return nil, fmt.Errorf("SHA256SUMS 中缺少有效的程序校验值")
	}
	if digest != "" && !strings.EqualFold(digest, "sha256:"+hex.EncodeToString(expected)) {
		return nil, fmt.Errorf("发布文件与 SHA256SUMS 校验值不一致")
	}
	release.Verification = &updater.Verification{DigestAlgo: "sha256", Digest: expected}
	// browser_download_url is unsuitable for authenticated private downloads.
	release.Metadata["github.asset.url"] = assetURL
	return release, nil
}

func (p *GitHubProvider) Download(ctx context.Context, release *updater.Release, dst io.Writer, progress func(int64, int64)) error {
	return p.inner.Download(ctx, release, dst, progress)
}
