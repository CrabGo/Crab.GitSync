package taskresult

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Failure is safe to expose to the UI; Cause remains private and preserves cancellation.
type Failure struct {
	Category  string `json:"category"`
	Stage     string `json:"stage"`
	Retryable bool   `json:"retryable"`
	Message   string `json:"message"`
	Detail    string `json:"detail"`
	cause     error
}

func (f *Failure) Error() string { return f.Message + ": " + f.Detail }
func (f *Failure) Unwrap() error { return f.cause }

var urls = regexp.MustCompile(`(?i)(?:https?|socks5h?)://[^\s"'<>]+`)

func Redact(s string) string {
	return urls.ReplaceAllStringFunc(s, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "[地址已隐藏]"
		}
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		return u.String()
	})
}
func Wrap(err error, stage string) *Failure {
	if err == nil {
		return nil
	}
	f := &Failure{Category: "unknown", Stage: stage, Message: "操作失败，请查看详细日志", Detail: Redact(err.Error()), cause: err}
	var previous *Failure
	if errors.As(err, &previous) {
		copy := *previous
		copy.Stage = stage
		copy.cause = err
		if stage == "refresh" && copy.Category != "cancelled" {
			copy.Category = "refresh"
			copy.Message = "操作已完成，但读取仓库状态失败"
			copy.Retryable = false
		}
		return &copy
	}
	s := strings.ToLower(err.Error())
	var dns *net.DNSError
	var network net.Error
	switch {
	case errors.Is(err, context.Canceled):
		f.Category = "cancelled"
		f.Message = "任务已取消"
	case errors.Is(err, context.DeadlineExceeded):
		f.Category = "timeout"
		f.Message = "请求超时，请检查网络或代理"
		f.Retryable = true
	case strings.Contains(s, "certificate") || strings.Contains(s, "ssl certificate") || strings.Contains(s, "tls handshake"):
		f.Category = "tls"
		f.Message = "TLS 连接或证书校验失败，请检查证书和代理"
	case strings.Contains(s, "authentication failed") || strings.Contains(s, "permission denied (publickey)") || strings.Contains(s, "could not read username") || strings.Contains(s, "could not read password") || strings.Contains(s, "http 401") || strings.Contains(s, "http 403") || strings.Contains(s, "returned error: 401") || strings.Contains(s, "returned error: 403") || strings.Contains(s, "proxy authentication"):
		f.Category = "authentication"
		f.Message = "认证失败，请检查 Git 凭据或访问权限"
	case errors.As(err, &dns) || strings.Contains(s, "could not resolve host"):
		f.Category = "dns"
		f.Message = "域名解析失败，请检查网络或代理 DNS"
		f.Retryable = true
	case strings.Contains(s, "could not resolve proxy") || strings.Contains(s, "proxyconnect") || strings.Contains(s, "failed to connect to proxy"):
		f.Category = "proxy"
		f.Message = "代理连接失败，请检查代理地址和运行状态"
		f.Retryable = true
	case errors.As(err, &network) && network.Timeout() || strings.Contains(s, "timed out") || strings.Contains(s, "operation timeout"):
		f.Category = "timeout"
		f.Message = "请求超时，请检查网络或代理"
		f.Retryable = true
	case strings.Contains(s, "repository not found") || strings.Contains(s, "does not appear to be a git repository") || strings.Contains(s, "http 404") || strings.Contains(s, "returned error: 404"):
		f.Category = "remote_not_found"
		f.Message = "远端不存在或无权访问，请核对远端地址与权限"
	case strings.Contains(s, "could not connect to server") || strings.Contains(s, "connection refused") || strings.Contains(s, "connection reset") || strings.Contains(s, "failed to connect") || strings.Contains(s, "http 502") || strings.Contains(s, "http 503") || strings.Contains(s, "http 504") || strings.Contains(s, "returned error: 502") || strings.Contains(s, "returned error: 503") || strings.Contains(s, "returned error: 504"):
		f.Category = "connection"
		f.Message = "网络连接失败，请检查网络或代理"
		f.Retryable = true
	case stage == "refresh":
		f.Category = "refresh"
		f.Message = "操作已完成，但读取仓库状态失败"
		f.Retryable = false
	case strings.Contains(s, "工作区") || strings.Contains(s, "跟踪分支") || strings.Contains(s, "合并未完成") || strings.Contains(s, "正在合并") || strings.Contains(s, "当前分支已变化") || strings.Contains(s, "请选择其他有效") || strings.Contains(s, "预览") || strings.Contains(s, "双方分叉") || strings.Contains(s, "本地分支") || strings.Contains(s, "没有跟踪分支"):
		f.Category = "git_state"
		f.Message = "仓库状态不允许操作，请检查分支、工作区或冲突"
	}
	// Refresh failures must never trigger another remote fetch.
	if stage == "refresh" && f.Category != "cancelled" {
		f.Category = "refresh"
		f.Message = "操作已完成，但读取仓库状态失败"
		f.Retryable = false
	}
	return f
}
func HTTPStatus(status int) error { return fmt.Errorf("HTTP %d", status) }

type Result struct {
	Remote           string   `json:"remote"`
	TaskID           string   `json:"taskID"`
	Kind             string   `json:"kind"`
	Path             string   `json:"path"`
	Branch           string   `json:"branch"`
	Stage            string   `json:"stage"`
	Status           string   `json:"status"`
	Attempts         int      `json:"attempts"`
	StartedAt        string   `json:"startedAt"`
	FinishedAt       string   `json:"finishedAt"`
	DurationMS       int64    `json:"durationMS"`
	NetworkSucceeded bool     `json:"networkSucceeded"`
	Failure          *Failure `json:"failure"`
}
