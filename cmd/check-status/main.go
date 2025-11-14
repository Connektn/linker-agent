package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	endpoint := os.Getenv("CONTROL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8081"
	}

	// Try to get status from health endpoint
	resp, err := http.Get(endpoint + "/healthz")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to agent: %v\n", err)
		fmt.Fprintf(os.Stderr, "Is the agent running? (./dist/linker-agent -webhook -config config.yaml)\n")
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read response: %v\n", err)
		os.Exit(1)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Agent Status:")
	fmt.Printf("  Health: %v\n", result["status"])
	fmt.Println()
	fmt.Println("Note: To see current mode, check the agent logs.")
	fmt.Println("The agent logs 'switching privacy mode from=X to=Y' when mode changes.")
	fmt.Println()
	fmt.Println("To switch mode:")
	fmt.Println("  make test-control-switch-mode    # Switch to passthrough")
	fmt.Println("  make test-control-switch-strict  # Switch to strict")
}
