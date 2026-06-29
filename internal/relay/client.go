package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/tarakeswararao/claude-agent/internal/claude"
	"github.com/tarakeswararao/claude-agent/internal/tools"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type Request struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Command string   `json:"command,omitempty"`
	Path    string   `json:"path,omitempty"`
	Content string   `json:"content,omitempty"`
	GitCmd  string   `json:"gitCmd,omitempty"`
	GitArgs []string `json:"gitArgs,omitempty"`
	Prompt  string   `json:"prompt,omitempty"`
	Session string   `json:"session,omitempty"`
}

type Response struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func Connect(relayURL string, token string) error {
	for {
		if err := connect(relayURL, token); err != nil {
			log.Printf("[agent] disconnected: %v — reconnecting in 3s", err)
			time.Sleep(3 * time.Second)
		}
	}
}

func connect(relayURL string, token string) error {
	ctx := context.Background()
	agentURL := fmt.Sprintf("%s/agent", relayURL)

	conn, _, err := websocket.Dial(ctx, agentURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"x-session-token": {token}},
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	log.Printf("[agent] connected to relay token=%s…", token[:8])

	for {
		var req Request
		if err := wsjson.Read(ctx, conn, &req); err != nil {
			return fmt.Errorf("read: %w", err)
		}

		go func(r Request) {
			resp := handle(r)
			if err := wsjson.Write(ctx, conn, resp); err != nil {
				log.Printf("[agent] write error: %v", err)
			}
		}(req)
	}
}

func handle(req Request) Response {
	resp := Response{ID: req.ID, OK: true}

	switch req.Type {
	case "exec":
		out, err := tools.RunCommand(req.Command)
		if err != nil {
			resp.OK = false
			resp.Error = err.Error()
			resp.Result = out
		} else {
			resp.Result = out
		}

	case "read":
		data, err := tools.ReadFile(req.Path)
		if err != nil {
			resp.OK = false
			resp.Error = err.Error()
		} else {
			resp.Result = data
		}

	case "write":
		if err := tools.WriteFile(req.Path, req.Content); err != nil {
			resp.OK = false
			resp.Error = err.Error()
		}

	case "list":
		entries, err := tools.ListDir(req.Path)
		if err != nil {
			resp.OK = false
			resp.Error = err.Error()
		} else {
			resp.Result = entries
		}

	case "git":
		out, err := tools.RunGit(req.GitCmd, req.GitArgs)
		if err != nil {
			resp.OK = false
			resp.Error = err.Error()
			resp.Result = out
		} else {
			resp.Result = out
		}

	case "chat":
		events := make(chan claude.StreamEvent, 32)
		if err := claude.Run(req.Prompt, req.Session, events); err != nil {
			resp.OK = false
			resp.Error = err.Error()
			return resp
		}
		// Collect all events into result
		var collected []claude.StreamEvent
		for e := range events {
			collected = append(collected, e)
		}
		raw, _ := json.Marshal(collected)
		resp.Result = json.RawMessage(raw)

	default:
		resp.OK = false
		resp.Error = fmt.Sprintf("unknown request type: %s", req.Type)
	}

	return resp
}
