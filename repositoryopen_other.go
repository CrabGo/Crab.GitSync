//go:build !windows

package main

import "fmt"

func openRepositoryDirectory(path, kind string) error {
	return fmt.Errorf("当前平台尚不支持此快捷操作")
}
