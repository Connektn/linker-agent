package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"linker-agent/internal/control"

	"github.com/google/uuid"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Parse command type
	cmdType := control.CommandType(os.Args[1])

	// Parse parameters
	params := make(map[string]interface{})
	for i := 2; i < len(os.Args); i++ {
		parts := strings.SplitN(os.Args[i], "=", 2)
		if len(parts) == 2 {
			params[parts[0]] = parts[1]
		}
	}

	// Get secret
	secret := os.Getenv("CONTROL_SECRET")
	if secret == "" {
		secret = "test-control-secret-key" // Default for testing
		fmt.Fprintf(os.Stderr, "Warning: CONTROL_SECRET not set, using default test secret\n")
	}

	// Get endpoint
	endpoint := os.Getenv("CONTROL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8081/api/control/command"
	}

	// Create command
	cmd := &control.Command{
		Command:   cmdType,
		Params:    params,
		Timestamp: time.Now().UTC(),
		Nonce:     uuid.New().String(),
	}

	// Sign command
	if err := cmd.Sign(secret); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to sign command: %v\n", err)
		os.Exit(1)
	}

	// Marshal to JSON
	body, err := json.MarshalIndent(cmd, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal command: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Sending command:")
	fmt.Println(string(body))
	fmt.Println()

	// Send request
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to send command: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read response: %v\n", err)
		os.Exit(1)
	}

	// Parse and display result
	var result control.CommandResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		fmt.Fprintf(os.Stderr, "Response: %s\n", string(respBody))
		os.Exit(1)
	}

	fmt.Printf("Response (HTTP %d):\n", resp.StatusCode)
	output, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(output))

	if !result.Success {
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s <command> [param=value...]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  stop              - Gracefully stop the agent\n")
	fmt.Fprintf(os.Stderr, "  start             - Start the agent (placeholder)\n")
	fmt.Fprintf(os.Stderr, "  restart           - Restart the agent\n")
	fmt.Fprintf(os.Stderr, "  switch_mode       - Switch privacy mode\n")
	fmt.Fprintf(os.Stderr, "  upgrade           - Upgrade agent version\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  %s stop\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s restart mode=graceful\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s switch_mode mode=passthrough\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s upgrade version=2.0.0\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Environment Variables:\n")
	fmt.Fprintf(os.Stderr, "  CONTROL_SECRET   - Signature secret (default: test-control-secret-key)\n")
	fmt.Fprintf(os.Stderr, "  CONTROL_ENDPOINT - API endpoint (default: http://localhost:8081/api/control/command)\n")
}
