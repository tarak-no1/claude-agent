package tools

import (
	"bytes"
	"fmt"
	"os/exec"
)

var allowedGitCmds = map[string]bool{
	"status": true,
	"log":    true,
	"diff":   true,
	"branch": true,
}

func RunGit(subcmd string, args []string) (string, error) {
	if !allowedGitCmds[subcmd] {
		return "", fmt.Errorf("git %s not allowed", subcmd)
	}
	cmdArgs := append([]string{subcmd}, args...)
	cmd := exec.Command("git", cmdArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}
