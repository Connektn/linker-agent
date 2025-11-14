# Connektn Linker Agent Makefile
.DEFAULT_GOAL := help

# Use SHELL to ensure environment is passed through
SHELL := /bin/bash

# Variables
BINARY_NAME := linker-agent
IMAGE_NAME := ghcr.io/connektn/linker-agent:latest
GO := go
MINIKUBE_NAMESPACE := connektn

# Colors for output
COLOR_RESET := \033[0m
COLOR_BOLD := \033[1m
COLOR_GREEN := \033[32m
COLOR_YELLOW := \033[33m
COLOR_BLUE := \033[34m

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\n$(COLOR_BOLD)Usage:$(COLOR_RESET)\n  make $(COLOR_BLUE)<target>$(COLOR_RESET)\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  $(COLOR_BLUE)%-20s$(COLOR_RESET) %s\n", $$1, $$2 } /^##@/ { printf "\n$(COLOR_BOLD)%s$(COLOR_RESET)\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: build
build: ## Build the Go binary
	@echo -e "$(COLOR_GREEN)Building $(BINARY_NAME)...$(COLOR_RESET)"
	$(GO) build -o $(BINARY_NAME) main.go
	@echo -e "$(COLOR_GREEN)✓ Build complete: $(BINARY_NAME)$(COLOR_RESET)"

.PHONY: run
run: ## Run the agent locally (requires STRIPE_API_KEY)
	@echo -e "$(COLOR_GREEN)Running $(BINARY_NAME)...$(COLOR_RESET)"
	@./run.sh

.PHONY: test
test: ## Run tests
	@echo -e "$(COLOR_GREEN)Running tests...$(COLOR_RESET)"
	$(GO) test -v ./...

.PHONY: test-coverage
test-coverage: ## Run tests with coverage
	@echo -e "$(COLOR_GREEN)Running tests with coverage...$(COLOR_RESET)"
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo -e "$(COLOR_GREEN)✓ Coverage report: coverage.html$(COLOR_RESET)"

.PHONY: fmt
fmt: ## Format Go code
	@echo -e "$(COLOR_GREEN)Formatting code...$(COLOR_RESET)"
	$(GO) fmt ./...

.PHONY: vet
vet: ## Run go vet
	@echo -e "$(COLOR_GREEN)Running go vet...$(COLOR_RESET)"
	$(GO) vet ./...

.PHONY: lint
lint: vet fmt ## Run linters (fmt + vet)
	@echo -e "$(COLOR_GREEN)✓ Linting complete$(COLOR_RESET)"

.PHONY: clean
clean: ## Remove build artifacts
	@echo -e "$(COLOR_GREEN)Cleaning build artifacts...$(COLOR_RESET)"
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	rm -rf seed_reports
	rm -f send-command
	@echo -e "$(COLOR_GREEN)✓ Clean complete$(COLOR_RESET)"

##@ Control Command Utilities

.PHONY: build-send-command
build-send-command: ## Build the send-command utility
	@echo -e "$(COLOR_GREEN)Building send-command utility...$(COLOR_RESET)"
	$(GO) build -o send-command cmd/send-command/main.go
	@echo -e "$(COLOR_GREEN)✓ Built: send-command$(COLOR_RESET)"

.PHONY: test-control-restart
test-control-restart: build-send-command ## Test control command: restart
	@echo -e "$(COLOR_GREEN)Sending restart command...$(COLOR_RESET)"
	./send-command restart

.PHONY: test-control-switch-mode
test-control-switch-mode: build-send-command ## Test control command: switch_mode to passthrough
	@echo -e "$(COLOR_GREEN)Sending switch_mode command (passthrough)...$(COLOR_RESET)"
	./send-command switch_mode mode=passthrough

.PHONY: test-control-switch-strict
test-control-switch-strict: build-send-command ## Test control command: switch_mode to strict
	@echo -e "$(COLOR_GREEN)Sending switch_mode command (strict)...$(COLOR_RESET)"
	./send-command switch_mode mode=strict

.PHONY: test-control-stop
test-control-stop: build-send-command ## Test control command: stop
	@echo -e "$(COLOR_GREEN)Sending stop command...$(COLOR_RESET)"
	./send-command stop

.PHONY: test-health
test-health: ## Test health check endpoint
	@echo -e "$(COLOR_GREEN)Testing health endpoint...$(COLOR_RESET)"
	@curl -s http://localhost:8081/healthz | jq . || echo "Agent not running or jq not installed"

##@ Production Verification

.PHONY: verify-build
verify-build: ## Verify production build succeeds
	@echo -e "$(COLOR_GREEN)Building production agent...$(COLOR_RESET)"
	$(GO) build -o dist/$(BINARY_NAME) main.go
	@echo -e "$(COLOR_GREEN)✓ Build successful$(COLOR_RESET)"
	@ls -lh dist/$(BINARY_NAME)

.PHONY: verify-agent-id
verify-agent-id: verify-build ## Verify agent ID persistence
	@./scripts/verify-agent-id.sh

.PHONY: verify-heartbeat-receiver
verify-heartbeat-receiver: ## Start mock heartbeat receiver (runs in foreground)
	@echo -e "$(COLOR_GREEN)Starting mock heartbeat receiver on :9000$(COLOR_RESET)"
	@echo -e "$(COLOR_YELLOW)Press Ctrl+C to stop$(COLOR_RESET)"
	@python3 -c "import json,hmac,hashlib;from http.server import HTTPServer,BaseHTTPRequestHandler;SECRET=b'test-heartbeat-secret';class H(BaseHTTPRequestHandler):\n def do_POST(s):d=json.loads(s.rfile.read(int(s.headers['Content-Length'])));p=f\"{d['agentId']}:{d['organizationId']}:{d['timestamp']}:{d['uptime']}:{d['mode']}:{d['queueDepth']}:{d['droppedCount']}:{d['enqueuedCount']}:{d['dlqSize']}\";e=hmac.new(SECRET,p.encode(),hashlib.sha256).hexdigest();print(f\"✅ HB: uptime={d['uptime']:3d}s mode={d['mode']}\" if d['signature']==e else '❌ Bad sig');s.send_response(200);s.send_header('Content-Type','application/json');s.end_headers();s.wfile.write(b'{\"status\":\"ok\"}')\n def log_message(s,*a):pass\nHTTPServer(('',9000),H).serve_forever()"

.PHONY: verify-all
verify-all: test verify-build ## Run all tests and verify production build
	@echo -e "$(COLOR_GREEN)✓ All verifications passed$(COLOR_RESET)"
	@echo ""
	@echo -e "$(COLOR_BLUE)Next steps:$(COLOR_RESET)"
	@echo "  1. See QUICKSTART-VERIFICATION.md for 10-minute verification workflow"
	@echo "  2. See VERIFICATION.md for comprehensive testing procedures"
	@echo "  3. See PRODUCTION-INTEGRATION.md for deployment guidance"

##@ Docker

.PHONY: docker-build
docker-build: ## Build Docker image
	@echo -e "$(COLOR_GREEN)Building Docker image...$(COLOR_RESET)"
	docker build -t $(IMAGE_NAME) .
	@echo -e "$(COLOR_GREEN)✓ Docker image built: $(IMAGE_NAME)$(COLOR_RESET)"

##@ Minikube

.PHONY: minikube-up
minikube-up: ## Start Minikube and deploy Connektn stack (requires env vars)
	@echo -e "$(COLOR_GREEN)Setting up Connektn on Minikube...$(COLOR_RESET)"
	@./scripts/minikube-setup.sh

.PHONY: minikube-down
minikube-down: ## Remove Connektn from Minikube
	@echo -e "$(COLOR_YELLOW)Cleaning up Connektn from Minikube...$(COLOR_RESET)"
	@./scripts/minikube-cleanup.sh

.PHONY: minikube-restart
minikube-restart: minikube-down minikube-up ## Restart Minikube deployment

.PHONY: minikube-status
minikube-status: ## Show Minikube deployment status
	@echo -e "$(COLOR_BLUE)Minikube Status:$(COLOR_RESET)"
	@minikube status || echo "Minikube not running"
	@echo ""
	@echo -e "$(COLOR_BLUE)Pods:$(COLOR_RESET)"
	@kubectl get pods -n $(MINIKUBE_NAMESPACE) || echo "Namespace not found"
	@echo ""
	@echo -e "$(COLOR_BLUE)Services:$(COLOR_RESET)"
	@kubectl get svc -n $(MINIKUBE_NAMESPACE) || echo "Namespace not found"

.PHONY: minikube-logs
minikube-logs: ## Show logs from Minikube pods
	@echo -e "$(COLOR_BLUE)Select which logs to view:$(COLOR_RESET)"
	@echo "  1) Agent logs"
	@echo "  2) Gateway logs"
	@echo "  3) Both (follow mode)"
	@read -p "Enter choice [1-3]: " choice; \
	case $$choice in \
		1) kubectl logs -n $(MINIKUBE_NAMESPACE) deployment/connektn-connektn-agent --tail=100 ;; \
		2) kubectl logs -n $(MINIKUBE_NAMESPACE) deployment/connektn-connektn-gateway --tail=100 ;; \
		3) kubectl logs -n $(MINIKUBE_NAMESPACE) -l app.kubernetes.io/instance=connektn -f ;; \
		*) echo "Invalid choice" ;; \
	esac

.PHONY: minikube-port-forward
minikube-port-forward: ## Port-forward the gateway (runs in foreground)
	@echo -e "$(COLOR_GREEN)Port-forwarding gateway to http://localhost:8080$(COLOR_RESET)"
	@echo -e "$(COLOR_YELLOW)Press Ctrl+C to stop$(COLOR_RESET)"
	@kubectl port-forward svc/connektn-connektn-gateway 8080:8080 -n $(MINIKUBE_NAMESPACE)

.PHONY: minikube-shell-agent
minikube-shell-agent: ## Debug shell into agent pod (distroless, uses kubectl debug)
	@echo -e "$(COLOR_BLUE)Launching debug shell for agent pod...$(COLOR_RESET)"
	@AGENT_POD=$$(kubectl get pod -n $(MINIKUBE_NAMESPACE) -l app.kubernetes.io/name=connektn-agent -o jsonpath='{.items[0].metadata.name}'); \
	echo "$(COLOR_GREEN)Debug pod will have access to agent filesystem at /proc/1/root/$(COLOR_RESET)"; \
	kubectl debug -it -n $(MINIKUBE_NAMESPACE) $$AGENT_POD --image=busybox:1.28 --target=agent

##@ Stripe

.PHONY: seed-stripe
seed-stripe: ## Seed Stripe with test data (requires STRIPE_API_KEY)
	@echo -e "$(COLOR_GREEN)Seeding Stripe with test data...$(COLOR_RESET)"
	@./scripts/seed_stripe_test_data.sh

##@ Utilities

.PHONY: check-env
check-env: ## Check required environment variables
	@echo -e "$(COLOR_BLUE)Checking environment variables...$(COLOR_RESET)"
	@if [ -z "$(STRIPE_API_KEY)" ]; then \
		echo -e "$(COLOR_YELLOW)⚠ STRIPE_API_KEY not set$(COLOR_RESET)"; \
	else \
		echo -e "$(COLOR_GREEN)✓ STRIPE_API_KEY is set$(COLOR_RESET)"; \
	fi
	@if [ -z "$(STRIPE_WEBHOOK_SECRET)" ]; then \
		echo -e "$(COLOR_YELLOW)⚠ STRIPE_WEBHOOK_SECRET not set$(COLOR_RESET)"; \
	else \
		echo -e "$(COLOR_GREEN)✓ STRIPE_WEBHOOK_SECRET is set$(COLOR_RESET)"; \
	fi
	@if [ -z "$(TENANT_SALT)" ]; then \
		echo -e "$(COLOR_YELLOW)⚠ TENANT_SALT not set$(COLOR_RESET)"; \
	else \
		echo -e "$(COLOR_GREEN)✓ TENANT_SALT is set$(COLOR_RESET)"; \
	fi

.PHONY: deps
deps: ## Download Go dependencies
	@echo -e "$(COLOR_GREEN)Downloading dependencies...$(COLOR_RESET)"
	$(GO) mod download
	@echo -e "$(COLOR_GREEN)✓ Dependencies downloaded$(COLOR_RESET)"

.PHONY: deps-update
deps-update: ## Update Go dependencies
	@echo -e "$(COLOR_GREEN)Updating dependencies...$(COLOR_RESET)"
	$(GO) get -u ./...
	$(GO) mod tidy
	@echo -e "$(COLOR_GREEN)✓ Dependencies updated$(COLOR_RESET)"
