package tools

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

func RunCommand(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := stdout.String() + stderr.String()
	if err != nil {
		return out, err
	}
	return out, nil
}
