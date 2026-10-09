package updatefeed

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"golang.org/x/mod/semver"
)

// PublicGitHub reads the stable release redirect and public assets without API quotas or credentials.
type PublicGitHub struct {
	repo, base string
	client     *http.Client
}

func NewPublicGitHub(repo, base string, client *http.Client) *PublicGitHub {
	if base == "" {
		base = "https://github.com"
	}
	if client == nil {
		client = NewHTTPClient()
	}
	return &PublicGitHub{repo: repo, base: strings.TrimRight(base, "/"), client: client}
}
func (p *PublicGitHub) Name() string { return "github-public" }
func (p *PublicGitHub) request(ctx context.Context, method, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Crab.GitSync-Updater")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接公开发布源，请检查网络或代理")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("公开发布源返回 HTTP %d，请检查发布文件或网络", resp.StatusCode)
	}
	return resp, nil
}
func (p *PublicGitHub) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	resp, err := p.request(ctx, http.MethodGet, p.base+"/"+p.repo+"/releases/latest")
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	prefix := p.base + "/" + p.repo + "/releases/tag/"
	final := resp.Request.URL.String()
	if !strings.HasPrefix(final, prefix) {
		return nil, fmt.Errorf("公开发布源未返回有效的稳定版本")
	}
	tag, err := url.PathUnescape(strings.TrimPrefix(final, prefix))
	if err != nil {
		return nil, err
	}
	version := "v" + strings.TrimPrefix(tag, "v")
	current := "v" + strings.TrimPrefix(req.CurrentVersion, "v")
	if !semver.IsValid(version) || !semver.IsValid(current) {
		return nil, fmt.Errorf("发布版本号格式无效")
	}
	if semver.Prerelease(version) != "" {
		return nil, fmt.Errorf("发布源返回了预发布版本")
	}
	if semver.Compare(version, current) <= 0 {
		return nil, nil
	}
	filename := "crab-gitsync-" + req.Platform + "-" + req.Arch
	if req.Platform == "windows" {
		filename += ".exe"
	}
	downloadBase := p.base + "/" + p.repo + "/releases/download/" + url.PathEscape(tag) + "/"
	resp, err = p.request(ctx, http.MethodGet, downloadBase+"SHA256SUMS")
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	resp.Body.Close()
	if err != nil || len(data) > 1<<20 {
		return nil, fmt.Errorf("无法读取发布校验文件")
	}
	var digest []byte
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == filename {
			digest, err = hex.DecodeString(fields[0])
			break
		}
	}
	if err != nil || len(digest) != 32 {
		return nil, fmt.Errorf("SHA256SUMS 中缺少有效的程序校验值")
	}
	assetURL := downloadBase + filename
	resp, err = p.request(ctx, http.MethodHead, assetURL)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	size := resp.ContentLength
	if size < 0 {
		size = 0
	}
	return &updater.Release{Version: strings.TrimPrefix(version, "v"), Notes: "版本说明请查看 GitHub 发布页面。", Artifact: updater.Artifact{Filename: filename, Platform: req.Platform, Arch: req.Arch, Size: size}, Verification: &updater.Verification{DigestAlgo: "sha256", Digest: digest}, Metadata: map[string]any{"download.url": assetURL}}, nil
}
func (p *PublicGitHub) Download(ctx context.Context, release *updater.Release, dst io.Writer, progress func(int64, int64)) error {
	endpoint, _ := release.Metadata["download.url"].(string)
	if !strings.HasPrefix(endpoint, p.base+"/"+p.repo+"/releases/download/") {
		return fmt.Errorf("无效的发布文件地址")
	}
	resp, err := p.request(ctx, http.MethodGet, endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	total := resp.ContentLength
	if total < 0 {
		total = 0
	}
	var written int64
	buffer := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			count, writeErr := dst.Write(buffer[:n])
			written += int64(count)
			if progress != nil {
				progress(written, total)
			}
			if writeErr != nil {
				return writeErr
			}
			if count != n {
				return io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
