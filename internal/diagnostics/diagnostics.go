// Package diagnostics performs read-only connection probes using a configuration snapshot.
package diagnostics

import (
	"context"
	"crab.gitsync/internal/gitengine"
	"crab.gitsync/internal/networksettings"
	"crab.gitsync/internal/taskresult"
	"crab.gitsync/internal/updatefeed"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Step struct {
	Name       string              `json:"name"`
	Status     string              `json:"status"`
	DurationMS int64               `json:"durationMS"`
	Info       string              `json:"info"`
	Failure    *taskresult.Failure `json:"failure"`
}
type State struct {
	Busy       bool   `json:"busy"`
	Status     string `json:"status"`
	Strategy   string `json:"strategy"`
	Remote     string `json:"remote"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
	Steps      []Step `json:"steps"`
}
type Probes struct {
	Dial func(context.Context, string) error
	HTTP func(context.Context, *url.URL) error
	Git  func(context.Context, string, *url.URL) error
}
type Runner struct {
	mu     sync.Mutex
	state  State
	cancel context.CancelFunc
	probes Probes
}

func New(probes Probes) *Runner {
	if probes.Dial == nil {
		probes.Dial = func(ctx context.Context, addr string) error {
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
			if err == nil {
				conn.Close()
			}
			return err
		}
	}
	if probes.HTTP == nil {
		probes.HTTP = func(ctx context.Context, proxy *url.URL) error {
			client := updatefeed.NewHTTPClientWithProxy(func() *url.URL { return proxy })
			defer client.CloseIdleConnections()
			req, _ := http.NewRequestWithContext(ctx, "HEAD", "https://github.com", nil)
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				return taskresult.HTTPStatus(resp.StatusCode)
			}
			return nil
		}
	}
	if probes.Git == nil {
		probes.Git = func(ctx context.Context, remote string, proxy *url.URL) error {
			if proxy != nil {
				ctx = gitengine.WithProxy(ctx, proxy.String())
			}
			_, err := gitengine.Run(ctx, os.TempDir(), 20*time.Second, "ls-remote", "--", remote, "HEAD")
			return err
		}
	}
	return &Runner{state: State{Status: "idle", Steps: []Step{}}, probes: probes}
}
func validRemote(remote string) (bool, error) {
	if strings.HasPrefix(remote, "git@") && strings.Contains(remote, ":") && !strings.ContainsAny(remote, "\r\n\t ") {
		return true, nil
	}
	u, err := url.Parse(remote)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return false, fmt.Errorf("请填写有效的 HTTP、HTTPS 或 SSH Git 仓库地址")
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "ssh" {
		return false, fmt.Errorf("诊断仅支持 HTTP、HTTPS 或 SSH 仓库")
	}
	if u.User != nil {
		if _, password := u.User.Password(); password || u.Scheme != "ssh" {
			return false, fmt.Errorf("诊断地址不要包含凭据，请使用本机 Git 认证配置")
		}
	}
	return u.Scheme == "ssh", nil
}
func (r *Runner) Start(config networksettings.Config, remote string) error {
	remote = strings.TrimSpace(remote)
	ssh, err := validRemote(remote)
	if err != nil {
		return err
	}
	proxy, err := config.URL()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Busy {
		return fmt.Errorf("连接诊断正在进行")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	r.cancel = cancel
	strategy := "沿用系统、环境变量及 Git 原有配置"
	if proxy != nil {
		strategy = "应用代理 · " + proxy.String()
	}
	r.state = State{Busy: true, Status: "running", Strategy: strategy, Remote: taskresult.Redact(remote), StartedAt: time.Now().Format(time.RFC3339), Steps: []Step{{Name: "代理端口", Status: "queued"}, {Name: "GitHub HTTPS", Status: "queued"}, {Name: "Git 远端访问", Status: "queued"}}}
	go r.run(ctx, cancel, config, proxy, remote, ssh)
	return nil
}
func (r *Runner) run(ctx context.Context, cancel context.CancelFunc, config networksettings.Config, proxy *url.URL, remote string, ssh bool) {
	defer cancel()
	for i := 0; i < 3; i++ {
		if ctx.Err() != nil {
			break
		}
		if i == 0 && proxy == nil {
			r.mu.Lock()
			r.state.Steps[i].Status = "skipped"
			r.state.Steps[i].Info = "应用代理已关闭，不测试指定代理端口"
			r.mu.Unlock()
			continue
		}
		r.mu.Lock()
		r.state.Steps[i].Status = "running"
		if i == 2 && ssh {
			r.state.Steps[i].Info = "SSH 仓库沿用本机 SSH 配置，HTTP 代理诊断不代表 SSH 认证成功"
		}
		r.mu.Unlock()
		started := time.Now()
		timeout := 20 * time.Second
		if i == 0 {
			timeout = 5 * time.Second
		}
		stepCtx, stop := context.WithTimeout(ctx, timeout)
		var err error
		switch i {
		case 0:
			err = r.probes.Dial(stepCtx, net.JoinHostPort(config.Host, fmt.Sprint(config.Port)))
			if err != nil {
				err = fmt.Errorf("proxyconnect tcp: %w", err)
			}
		case 1:
			err = r.probes.HTTP(stepCtx, proxy)
		case 2:
			err = r.probes.Git(stepCtx, remote, proxy)
		}
		if err == nil && stepCtx.Err() != nil {
			err = stepCtx.Err()
		}
		stop()
		stage := []string{"proxy", "https", "git-remote"}[i]
		failure := taskresult.Wrap(err, stage)
		if i == 0 && failure != nil && failure.Category == "connection" {
			failure.Category = "proxy"
			failure.Message = "代理端口无法连接，请检查代理程序是否已启动"
		}
		r.mu.Lock()
		step := &r.state.Steps[i]
		step.DurationMS = time.Since(started).Milliseconds()
		step.Failure = failure
		step.Status = "success"
		if err != nil {
			step.Status = "error"
		}
		if ctx.Err() != nil {
			step.Status = "cancelled"
			step.Failure = taskresult.Wrap(ctx.Err(), stage)
		}
		r.mu.Unlock()
		// Continue independent probes: a failed HTTP probe must not imply Git authentication failed.
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Busy = false
	r.state.Status = "success"
	r.state.FinishedAt = time.Now().Format(time.RFC3339)
	for i := range r.state.Steps {
		if r.state.Steps[i].Status == "error" {
			r.state.Status = "error"
		}
		if r.state.Steps[i].Status == "queued" {
			r.state.Steps[i].Status = "cancelled"
			r.state.Steps[i].Failure = taskresult.Wrap(ctx.Err(), "queued")
		}
	}
	if ctx.Err() != nil {
		r.state.Status = "cancelled"
	}
	r.cancel = nil
}
func (r *Runner) State() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := r.state
	copy.Steps = append([]Step{}, copy.Steps...)
	for i := range copy.Steps {
		if copy.Steps[i].Failure != nil {
			f := *copy.Steps[i].Failure
			copy.Steps[i].Failure = &f
		}
	}
	return copy
}
func (r *Runner) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}
