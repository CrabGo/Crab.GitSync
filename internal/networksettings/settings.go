package networksettings

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Config struct {
	Enabled   bool   `json:"enabled"`
	Protocol  string `json:"protocol"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	AutoRetry bool   `json:"autoRetry"`
}

func Default() Config { return Config{Enabled: true, Protocol: "http", Host: "127.0.0.1", Port: 33210} }

var hostname = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`)

func (c Config) URL() (*url.URL, error) {
	if c.Protocol != "http" && c.Protocol != "socks5h" {
		return nil, fmt.Errorf("代理类型必须为 HTTP 或 SOCKS5")
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, fmt.Errorf("代理端口必须为 1–65535")
	}
	if c.Host == "" || (net.ParseIP(c.Host) == nil && !hostname.MatchString(c.Host)) {
		return nil, fmt.Errorf("请输入有效代理主机，不要填写协议或路径")
	}
	if !c.Enabled {
		return nil, nil
	}
	return url.Parse(c.Protocol + "://" + net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
}

type Store struct {
	mu     sync.RWMutex
	config Config
	path   string
}

func New(path string) (*Store, error) {
	s := &Store{config: Default(), path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("无法读取代理设置：%w", err)
	}
	var c Config
	if err = json.Unmarshal(data, &c); err != nil {
		return s, fmt.Errorf("代理设置文件无效：%w", err)
	}
	if _, err = c.URL(); err != nil {
		return s, err
	}
	s.config = c
	return s, nil
}
func (s *Store) Get() Config { s.mu.RLock(); defer s.mu.RUnlock(); return s.config }
func (s *Store) Save(c Config) error {
	c.Host = strings.TrimSpace(c.Host)
	if _, err := c.URL(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("无法保存代理设置：%w", err)
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	f, err := os.CreateTemp(filepath.Dir(s.path), "network-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	s.config = c
	return nil
}
