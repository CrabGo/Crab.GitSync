//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func repositoryCommand(path, kind string) (*exec.Cmd, error) {
	switch kind {
	case "folder":
		return exec.Command("explorer.exe", path), nil
	case "terminal":
		cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NoExit", "-Command", "Set-Location -LiteralPath '"+strings.ReplaceAll(path, "'", "''")+"'")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010}
		return cmd, nil
	default:
		return nil, fmt.Errorf("未知快捷操作")
	}
}
func openRepositoryDirectory(path, kind string) error {
	cmd, err := repositoryCommand(path, kind)
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("无法打开目录或终端：%w", err)
	}
	go cmd.Wait()
	return nil
}
