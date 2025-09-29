# podboard - Kubernetes Pod Dashboard

A web-based dashboard for monitoring Kubernetes pods in real-time, providing a visual interface similar to `kubectl get pod --watch`.

## Features

- 🚀 **Real-time Pod Monitoring**: Live view of pod status across namespaces
- 🔍 **Multi-Namespace Support**: Filter and view pods across different namespaces
- 🏷️ **Label Selector Filtering**: Advanced filtering with regex support (use `=~` operator)
- 🗑️ **Pod Management**: Delete pods directly from the web interface
- 🔄 **Configurable Refresh**: Adjustable refresh intervals (2s default)
- 🌐 **Multi-Cluster**: Cluster selector for multi-cluster environments
- 🐳 **Docker Ready**: Single binary with embedded web UI

## Installation

### Go Install (Recommended)
```bash
go install github.com/nikogura/podboard@latest
```

### Binary Download
Download pre-built binaries from the [releases page](https://github.com/nikogura/podboard/releases).

### Docker
```bash
docker run -p 9999:9999 ghcr.io/nikogura/podboard:latest
```

## Quick Start

### Local Development
```bash
# Uses ~/.kube/config automatically
podboard

# Custom port and domain
podboard --bind-address=0.0.0.0:8080 --domain=podboard.example.com
```

### Kubernetes Deployment
```bash
# Deploy with service account
kubectl apply -f k8s/
```

## Configuration

### Command Line Options
- `--bind-address` (`-b`): Server address and port (default: `0.0.0.0:9999`)
- `--domain` (`-d`): Server domain name for cookies
- `--verbose` (`-v`): Enable verbose logging
- `--log-level` (`-l`): Set log level (Trace, Debug, Info, Warn, Error)

### Environment Variables
- `DOMAIN`: Application domain for cookies
- `NAMESPACE`: Default namespace to monitor (default: `default`)

### Kubernetes Configuration
- **In-cluster**: Automatically uses in-cluster service account
- **Local**: Falls back to `~/.kube/config` for development

## API Endpoints

### Health & Status
- `GET /health` - Health check endpoint

### Pod Management
- `GET /api/pods` - List pods in namespace
  - Query params: `cluster`, `namespace`, `labelSelector`
- `DELETE /api/pods/:namespace/:name` - Delete a pod
  - Query params: `cluster`

### Cluster & Namespace Discovery
- `GET /api/clusters` - Available clusters (local mode only)
- `GET /api/namespaces` - Available namespaces
  - Query params: `cluster`

## Usage Examples

### Basic Monitoring
```bash
# Monitor default namespace
podboard

# Access web UI
open http://localhost:9999
```

### Label Filtering
Use the web UI to filter pods by labels:
- `app=nginx` - Exact match
- `app=~web.*` - Regex match (any label starting with "web")
- `environment!=production` - Negative match

### Multi-Cluster Setup
When running locally with multiple clusters in `~/.kube/config`:
1. Select cluster from dropdown
2. Choose namespace
3. Apply label filters as needed

## Development

### Build from Source
```bash
# Clone repository
git clone https://github.com/nikogura/podboard.git
cd podboard

# Build everything (UI + Go binary)
make build

# Development with live reload
make dev-ui    # Start UI dev server (port 3000)
make dev-server # Start Go server (port 3001)
```

### Testing
```bash
# Run unit tests
make test

# Run integration tests
make test-integration

# Linting
make lint
```

### Project Structure
```
podboard/
├── cmd/                 # CLI commands (Cobra)
├── pkg/
│   ├── podboard/       # Core server logic
│   └── ui/            # React/Next.js frontend
├── k8s/               # Kubernetes manifests
└── Makefile          # Build automation
```

## Architecture

```
┌─────────────────┐    HTTP     ┌─────────────────┐    Kubernetes API    ┌─────────────────┐
│   Web Browser   │◄───────────►│   podboard      │◄───────────────────►│ Kubernetes      │
│                 │             │   Server        │                     │ Cluster(s)      │
└─────────────────┘             └─────────────────┘                     └─────────────────┘
                                          │
                                          ▼
                                ┌─────────────────┐
                                │ Embedded React  │
                                │ UI (Static)     │
                                └─────────────────┘
```

### Components
- **Web Server**: Gin-based HTTP server with embedded static assets
- **Kubernetes Client**: Uses `client-go` for cluster communication
- **Web UI**: React frontend with real-time updates
- **Multi-Cluster**: Support for multiple kubeconfig contexts

## Security

- Service account permissions required for in-cluster deployment
- Local development uses existing kubeconfig permissions
- No authentication required (intended for trusted networks)
- Pod deletion operations require appropriate RBAC permissions

## Troubleshooting

### Common Issues

1. **Permission Denied**
   ```bash
   # Check RBAC permissions
   kubectl auth can-i list pods
   kubectl auth can-i delete pods
   ```

2. **Connection Issues**
   ```bash
   # Verify kubeconfig
   kubectl cluster-info
   kubectl get pods
   ```

3. **Build Issues**
   ```bash
   # Clean and rebuild
   make clean
   make build
   ```

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/new-feature`
3. Make changes and add tests
4. Run tests: `make test lint`
5. Commit changes: `git commit -am 'Add new feature'`
6. Push to branch: `git push origin feature/new-feature`
7. Create Pull Request

## License

Copyright (c) 2024 Nik Ogura. Licensed under the MIT License.