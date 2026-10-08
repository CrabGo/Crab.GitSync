//go:build !windows

package updatefeed

import (
	"net/http"
	"net/url"
	"os/exec"
)

func hideWindow(cmd *exec.Cmd)                               {}
func proxyForRequest() func(*http.Request) (*url.URL, error) { return http.ProxyFromEnvironment }
