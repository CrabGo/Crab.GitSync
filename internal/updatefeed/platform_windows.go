//go:build windows

package updatefeed

import (
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

func hideWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

// Go's default transport does not read the user's Windows proxy settings.
func proxyForRequest() func(*http.Request) (*url.URL, error) {
	for _, key := range []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy"} {
		if os.Getenv(key) != "" {
			return http.ProxyFromEnvironment
		}
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return http.ProxyFromEnvironment
	}
	defer key.Close()
	enabled, _, _ := key.GetIntegerValue("ProxyEnable")
	server, _, _ := key.GetStringValue("ProxyServer")
	if enabled != 1 || server == "" {
		return http.ProxyFromEnvironment
	}
	return func(req *http.Request) (*url.URL, error) {
		host := req.URL.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil, nil
		}
		value := server
		if strings.Contains(server, "=") {
			value = ""
			for _, part := range strings.Split(server, ";") {
				pair := strings.SplitN(part, "=", 2)
				if len(pair) == 2 && pair[0] == req.URL.Scheme {
					value = pair[1]
				}
			}
		}
		if value == "" {
			return nil, nil
		}
		if !strings.Contains(value, "://") {
			value = "http://" + value
		}
		return url.Parse(value)
	}
}
