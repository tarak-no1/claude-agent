package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	qrterminal "github.com/mdp/qrterminal/v3"
	"github.com/tarakeswararao/claude-agent/internal/relay"
)

const defaultRelayURL = "wss://claude-agent-relay-production.up.railway.app"

// version is set at build time via -ldflags="-X main.version=v1.2.3"
var version = "dev"

type savedConfig struct {
	Token string `json:"token"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claude-agent", "config.json")
}

func loadOrCreateToken() string {
	path := configPath()

	// Load existing token
	if data, err := os.ReadFile(path); err == nil {
		var cfg savedConfig
		if json.Unmarshal(data, &cfg) == nil && cfg.Token != "" {
			return cfg.Token
		}
	}

	// Generate new token
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)

	// Persist it
	os.MkdirAll(filepath.Dir(path), 0700)
	data, _ := json.Marshal(savedConfig{Token: token})
	os.WriteFile(path, data, 0600)

	return token
}

func printQR(token string, relayURL string) {
	// QR payload: JSON the app scans and parses
	payload, _ := json.Marshal(map[string]string{
		"token": token,
		"relay": strings.Replace(relayURL, "wss://", "https://", 1),
	})

	fmt.Println()
	fmt.Println("  Claude Personal Assistant — Mac Agent")
	fmt.Println("  ──────────────────────────────────────")
	fmt.Println()
	qrterminal.GenerateWithConfig(string(payload), qrterminal.Config{
		Level:     qrterminal.L,
		Writer:    os.Stdout,
		BlackChar: qrterminal.BLACK,
		WhiteChar: qrterminal.WHITE,
		QuietZone: 2,
	})
	fmt.Println()
	fmt.Printf("  Token: %s\n", token)
	fmt.Println()
	fmt.Println("  Scan the QR code with the Claude Personal Assistant app")
	fmt.Println("  or copy the token into Settings → Connect.")
	fmt.Println()
	fmt.Println("  Waiting for phone to connect…")
	fmt.Println()
}

func main() {
	relayURL := defaultRelayURL

	// Optional: explicit token or relay URL as args (for testing)
	token := ""
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "wss://") || strings.HasPrefix(arg, "ws://") ||
			strings.HasPrefix(arg, "https://") || strings.HasPrefix(arg, "http://") {
			relayURL = arg
		} else if arg == "--version" || arg == "-v" {
			fmt.Printf("claude-agent %s\n", version)
			os.Exit(0)
		} else {
			token = arg
		}
	}

	relayURL = strings.Replace(relayURL, "https://", "wss://", 1)
	relayURL = strings.Replace(relayURL, "http://", "ws://", 1)

	// Use saved/generated token if none provided
	if token == "" {
		token = loadOrCreateToken()
	}

	printQR(token, relayURL)

	log.Printf("[agent] relay=%s  token=%s…", relayURL, token[:min(8, len(token))])
	if err := relay.Connect(relayURL, token); err != nil {
		log.Fatalf("[agent] fatal: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
