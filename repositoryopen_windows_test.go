//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestRepositoryShortcutUsesSafeProcessArguments(t *testing.T) {
	path := `C:\work\a' & $(Write-Output attack) ` + "`" + `quoted`
	terminal, err := repositoryCommand(path, "terminal")
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Args[0] != "powershell.exe" || !strings.Contains(terminal.Args[len(terminal.Args)-1], "-LiteralPath 'C:\\work\\a'' & $(Write-Output attack)") {
		t.Fatal(terminal.Args)
	}
	if terminal.SysProcAttr == nil || terminal.SysProcAttr.CreationFlags != 0x10 {
		t.Fatal("terminal does not use a separate visible console")
	}
	folder, err := repositoryCommand(path, "folder")
	if err != nil || len(folder.Args) != 2 || folder.Args[1] != path {
		t.Fatal("folder path was shell-interpolated", folder, err)
	}
}
