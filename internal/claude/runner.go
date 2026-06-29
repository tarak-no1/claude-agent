package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
)

type StreamEvent struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

// Run spawns the claude CLI and streams parsed events to the out channel.
// The caller is responsible for reading until the channel is closed.
func Run(prompt string, sessionID string, out chan<- StreamEvent) error {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude CLI not found: %w", err)
	}

	args := []string{"--print", "--output-format", "stream-json", "--verbose"}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	args = append(args, "--dangerously-skip-permissions", prompt)

	cmd := exec.Command(claudePath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		defer close(out)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Bytes()
			var raw map[string]any
			if err := json.Unmarshal(line, &raw); err != nil {
				continue
			}
			eventType, _ := raw["type"].(string)
			out <- StreamEvent{Type: eventType, Payload: raw}
		}
		cmd.Wait()
	}()

	return nil
}
