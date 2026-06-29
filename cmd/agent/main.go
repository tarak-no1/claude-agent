package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/tarakeswararao/claude-agent/internal/relay"
)

const defaultRelayURL = "wss://claude-agent-relay-production.up.railway.app"

// version is set at build time via -ldflags="-X main.version=v1.2.3"
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "claude-agent %s\n", version)
		fmt.Fprintf(os.Stderr, "Usage: claude-agent <session-token> [relay-url]\n")
		fmt.Fprintf(os.Stderr, "  session-token  — token shown in the QR code on your phone\n")
		fmt.Fprintf(os.Stderr, "  relay-url      — optional, defaults to %s\n", defaultRelayURL)
		os.Exit(1)
	}

	token := os.Args[1]
	relayURL := defaultRelayURL
	if len(os.Args) >= 3 {
		relayURL = os.Args[2]
	}

	// Accept https:// URLs and convert to wss://
	relayURL = strings.Replace(relayURL, "https://", "wss://", 1)
	relayURL = strings.Replace(relayURL, "http://", "ws://", 1)

	log.Printf("[agent] starting  relay=%s  token=%s…", relayURL, token[:min(8, len(token))])
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
