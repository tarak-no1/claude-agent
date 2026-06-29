package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Wire frame from mobile app
type InFrame struct {
	// common
	Type string `json:"type"`
	// message.send
	SessionID string `json:"sessionId,omitempty"`
	Content   string `json:"content,omitempty"`
	// session.create / session.list / session.messages / session.delete
}

// Wire frame pushed back to mobile
type OutFrame struct {
	Event   string `json:"event"`
	Payload any    `json:"payload"`
}

// In-memory session store (survives reconnects within same agent run)
type session struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	ClaudeSession string    `json:"-"` // --resume id
}

var (
	sessionsMu sync.Mutex
	sessions   = map[string]*session{}
)

func newSession() *session {
	id := fmt.Sprintf("session-%d", time.Now().UnixMilli())
	s := &session{ID: id, Title: "New Chat", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	sessionsMu.Lock()
	sessions[id] = s
	sessionsMu.Unlock()
	return s
}

func getSession(id string) *session {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	return sessions[id]
}

func allSessions() []*session {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	out := make([]*session, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s)
	}
	return out
}

// Connect loops forever, reconnecting on disconnect.
func Connect(relayURL string, token string) error {
	for {
		if err := connect(relayURL, token); err != nil {
			log.Printf("[agent] disconnected: %v — reconnecting in 3s", err)
			time.Sleep(3 * time.Second)
		}
	}
}

func connect(relayURL string, token string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("%s/agent", relayURL), &websocket.DialOptions{
		HTTPHeader: http.Header{"x-session-token": {token}},
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	log.Printf("[agent] connected  token=%s…", token[:8])

	// Keepalive: send {type:"ping"} every 20s to prevent Railway idle disconnect
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				wsjson.Write(ctx, conn, map[string]string{"type": "ping"})
			}
		}
	}()

	for {
		var frame InFrame
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			return fmt.Errorf("read: %w", err)
		}

		// Absorb pong replies from relay
		if frame.Type == "pong" {
			continue
		}

		go dispatch(ctx, conn, frame)
	}
}

func push(ctx context.Context, conn *websocket.Conn, event string, payload any) {
	if err := wsjson.Write(ctx, conn, OutFrame{Event: event, Payload: payload}); err != nil {
		log.Printf("[agent] push %s error: %v", event, err)
	}
}

func dispatch(ctx context.Context, conn *websocket.Conn, f InFrame) {
	switch f.Type {

	case "session.create":
		s := newSession()
		push(ctx, conn, "session.created", map[string]any{"session": s})

	case "session.list":
		push(ctx, conn, "session.list.result", map[string]any{
			"sessions":             allSessions(),
			"activeSessionIds":     []string{},
			"interruptedSessionIds": []string{},
		})

	case "session.messages":
		push(ctx, conn, "session.messages.result", map[string]any{
			"sessionId": f.SessionID,
			"messages":  []any{},
		})

	case "session.delete":
		sessionsMu.Lock()
		delete(sessions, f.SessionID)
		sessionsMu.Unlock()
		push(ctx, conn, "session.deleted", map[string]any{"sessionId": f.SessionID})

	case "session.resume":
		push(ctx, conn, "session.resume.result", map[string]any{
			"sessionId": f.SessionID,
			"isActive":  false,
		})

	case "message.send":
		handleChat(ctx, conn, f)

	case "message.stop":
		// No-op for now — would need per-session cancel context

	// File events — delegate to local filesystem
	case "file.tree":
		handleFileTree(ctx, conn, f)

	case "file.read":
		handleFileRead(ctx, conn, f)

	case "git.status":
		handleGit(ctx, conn, "git.status.result", []string{"status", "--short"}, f)

	case "git.log":
		handleGit(ctx, conn, "git.log.result", []string{"log", "--oneline", "-20"}, f)

	default:
		log.Printf("[agent] unhandled event: %s", f.Type)
	}
}

func handleChat(ctx context.Context, conn *websocket.Conn, f InFrame) {
	s := getSession(f.SessionID)
	if s == nil {
		s = newSession()
		s.ID = f.SessionID
		sessionsMu.Lock()
		sessions[f.SessionID] = s
		sessionsMu.Unlock()
	}

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		push(ctx, conn, "error", map[string]any{"message": "claude CLI not found"})
		return
	}

	args := []string{"--print", "--output-format", "stream-json", "--verbose", "--dangerously-skip-permissions"}
	if s.ClaudeSession != "" {
		args = append(args, "--resume", s.ClaudeSession)
	}
	args = append(args, f.Content)

	cmd := exec.Command(claudePath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		push(ctx, conn, "error", map[string]any{"message": err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		push(ctx, conn, "error", map[string]any{"message": err.Error()})
		return
	}

	// Signal thinking
	push(ctx, conn, "message.thinking", map[string]any{"sessionId": f.SessionID})

	messageID := fmt.Sprintf("msg-%d", time.Now().UnixMilli())
	var fullText string

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var raw map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}

		eventType, _ := raw["type"].(string)

		switch eventType {
		case "assistant":
			// Extract text tokens from content blocks
			message, _ := raw["message"].(map[string]any)
			if message == nil {
				continue
			}
			content, _ := message["content"].([]any)
			for _, block := range content {
				b, _ := block.(map[string]any)
				if b == nil {
					continue
				}
				if b["type"] == "text" {
					text, _ := b["text"].(string)
					if text == "" {
						continue
					}
					fullText += text
					push(ctx, conn, "message.stream", map[string]any{
						"sessionId": f.SessionID,
						"messageId": messageID,
						"token":     text,
					})
				}
			}

		case "result":
			// Persist the claude session id for --resume
			if sid, ok := raw["session_id"].(string); ok && sid != "" {
				sessionsMu.Lock()
				if sessions[f.SessionID] != nil {
					sessions[f.SessionID].ClaudeSession = sid
					sessions[f.SessionID].UpdatedAt = time.Now()
				}
				sessionsMu.Unlock()
			}

			costUSD, _ := raw["total_cost_usd"].(float64)
			numTurns, _ := raw["num_turns"].(float64)

			push(ctx, conn, "message.complete", map[string]any{
				"sessionId": f.SessionID,
				"message": map[string]any{
					"id":        messageID,
					"sessionId": f.SessionID,
					"role":      "assistant",
					"content":   fullText,
					"createdAt": time.Now(),
				},
				"costUSD":  costUSD,
				"numTurns": int(numTurns),
			})

			// Update session title from first message
			sessionsMu.Lock()
			if sessions[f.SessionID] != nil && sessions[f.SessionID].Title == "New Chat" && len(fullText) > 0 {
				title := fullText
				if len(title) > 50 {
					title = title[:50] + "…"
				}
				sessions[f.SessionID].Title = title
			}
			sessionsMu.Unlock()

			push(ctx, conn, "session.updated", map[string]any{"session": sessions[f.SessionID]})
		}
	}

	cmd.Wait()
}

func handleFileTree(ctx context.Context, conn *websocket.Conn, f InFrame) {
	// Simple directory listing via ls
	var raw map[string]any
	data, _ := json.Marshal(f)
	json.Unmarshal(data, &raw)
	path, _ := raw["path"].(string)
	if path == "" {
		path = "."
	}
	out, err := runCmd("ls", "-la", path)
	if err != nil {
		push(ctx, conn, "file.error", map[string]any{"message": err.Error()})
		return
	}
	push(ctx, conn, "file.tree.result", map[string]any{"entries": out, "rootPath": path})
}

func handleFileRead(ctx context.Context, conn *websocket.Conn, f InFrame) {
	var raw map[string]any
	data, _ := json.Marshal(f)
	json.Unmarshal(data, &raw)
	path, _ := raw["path"].(string)
	out, err := runCmd("cat", path)
	if err != nil {
		push(ctx, conn, "file.error", map[string]any{"message": err.Error()})
		return
	}
	push(ctx, conn, "file.content", map[string]any{"path": path, "content": out})
}

func handleGit(ctx context.Context, conn *websocket.Conn, resultEvent string, args []string, f InFrame) {
	out, err := runCmd("git", args...)
	if err != nil {
		push(ctx, conn, "error", map[string]any{"message": err.Error()})
		return
	}
	push(ctx, conn, resultEvent, map[string]any{"output": out})
}

func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
