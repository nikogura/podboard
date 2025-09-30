.PHONY: build build-ui build-go test test-integration test-integration-real test-binary test-docker test-k8s test-install test-all clean docker-build docker-push help

# Variables
BINARY_NAME=podboard
DOCKER_REPO?=ghcr.io/nikogura/podboard
VERSION?=latest

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: build-ui build-go ## Build the complete application

build-ui: ## Build the UI
	cd pkg/ui && npm ci && npm run build

build-go: ## Build the Go binary
	CGO_ENABLED=0 go build -a -installsuffix cgo -o $(BINARY_NAME) .

test: ## Run unit tests
	go test -v ./...

test-integration: ## Run integration tests with mocks
	go test -v ./test -run TestPodboardIntegration

test-integration-real: ## Run integration tests against real infrastructure
	@echo "⚠️  Running tests against real infrastructure!"
	@echo "Required environment variables:"
	@echo "  - PODBOARD_REAL_TEST=true"
	@echo "  - PODBOARD_SERVER_URL (optional, defaults to http://localhost:9999)"
	@echo "  - KUBERNETES_CONFIG (path to kubeconfig, defaults to ~/.kube/config)"
	@echo "  - NAMESPACE (default namespace, defaults to 'default')"
	@echo ""
	PODBOARD_REAL_TEST=true go test -v ./test -run TestPodboardRealInfrastructure

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME)
	cd pkg/ui && rm -rf dist node_modules

docker-build: ## Build Docker image
	docker build -t $(DOCKER_REPO):$(VERSION) .

docker-push: docker-build ## Build and push Docker image
	docker push $(DOCKER_REPO):$(VERSION)

dev-server: ## Start development server (requires manual UI build)
	@echo "Starting development server..."
	@echo "Make sure to build the UI first: make build-ui"
	go run . server --bind-address=0.0.0.0:3001

dev-ui: ## Start UI development server
	cd pkg/ui && npm run dev

lint: ## Run linters
	golangci-lint run
	cd pkg/ui && npm run lint

test-binary: build ## Test built binaries (requires PODBOARD_BINARY_TEST=true)
	@echo "Testing binary releases..."
	@echo "Set PODBOARD_BINARY_TEST=true to enable binary tests"
	PODBOARD_BINARY_TEST=true RELEASE_ASSETS_DIR=release-assets go test -v ./test -run TestBinarySmoke
	PODBOARD_BINARY_TEST=true RELEASE_ASSETS_DIR=release-assets go test -v ./test -run TestBinaryStartupSmoke

test-docker: ## Test Docker functionality (requires PODBOARD_DOCKER_TEST=true)
	@echo "Testing Docker image and compose..."
	@echo "Set PODBOARD_DOCKER_TEST=true to enable Docker tests"
	PODBOARD_DOCKER_TEST=true go test -v ./test -run TestDockerImage
	PODBOARD_DOCKER_TEST=true go test -v ./test -run TestDockerCompose

test-k8s: ## Test Kubernetes manifests (requires PODBOARD_K8S_TEST=true)
	@echo "Testing Kubernetes manifests..."
	@echo "Set PODBOARD_K8S_TEST=true to enable K8s tests"
	PODBOARD_K8S_TEST=true go test -v ./test -run TestKubernetesManifests
	PODBOARD_K8S_TEST=true go test -v ./test -run TestRBACPermissions
	PODBOARD_K8S_TEST=true go test -v ./test -run TestDeploymentManifest
	PODBOARD_K8S_TEST=true go test -v ./test -run TestAllInOneManifest

test-install: ## Test installation script (requires PODBOARD_INSTALL_TEST=true)
	@echo "Testing installation script..."
	@echo "Set PODBOARD_INSTALL_TEST=true to enable install script tests"
	PODBOARD_INSTALL_TEST=true go test -v ./test -run TestInstallScript

test-all: test test-integration ## Run all available tests (requires environment variables for comprehensive tests)
	@echo "Running comprehensive test suite..."
	@echo "Note: Set PODBOARD_*_TEST=true environment variables to enable specific test suites"
	-PODBOARD_BINARY_TEST=true RELEASE_ASSETS_DIR=release-assets go test -v ./test -run TestBinarySmoke
	-PODBOARD_DOCKER_TEST=true go test -v ./test -run TestDockerImage
	-PODBOARD_K8S_TEST=true go test -v ./test -run TestKubernetesManifests
	-PODBOARD_INSTALL_TEST=true go test -v ./test -run TestInstallScript