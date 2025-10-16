# Bootstrap development environment by installing Go and dependencies
bootstrap:
    #!/usr/bin/env bash
    set -euo pipefail
    
    echo "🚀 Bootstrapping asimi-cli development environment..."
    
    # Check if Go is already installed
    if command -v go &> /dev/null; then
        echo "✓ Go is already installed: $(go version)"
    else
        echo "📦 Installing Go..."
        
        # Detect OS and architecture
        OS=$(uname -s | tr '[:upper:]' '[:lower:]')
        ARCH=$(uname -m)
        
        # Map architecture names
        case "$ARCH" in
            x86_64)
                ARCH="amd64"
                ;;
            aarch64|arm64)
                ARCH="arm64"
                ;;
            armv7l)
                ARCH="armv6l"
                ;;
        esac
        
        # Get latest Go version
        GO_VERSION=$(curl -sL https://go.dev/VERSION?m=text | head -1)
        echo "  Latest Go version: $GO_VERSION"
        
        # Download and install Go
        GO_TARBALL="${GO_VERSION}.${OS}-${ARCH}.tar.gz"
        GO_URL="https://go.dev/dl/${GO_TARBALL}"
        
        echo "  Downloading from $GO_URL..."
        wget -q "$GO_URL" || curl -sLO "$GO_URL"
        
        echo "  Installing to /usr/local/go..."
        sudo tar -C /usr/local -xzf "$GO_TARBALL"
        rm "$GO_TARBALL"
        
        # Add Go to PATH in bashrc if not already present
        if ! grep -q "/usr/local/go/bin" ~/.bashrc; then
            echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
            echo "  Added Go to PATH in ~/.bashrc"
        fi
        
        # Add Go to current session PATH
        export PATH=$PATH:/usr/local/go/bin
        
        echo "✓ Go installed successfully: $(go version)"
    fi
    
    # Verify Go is in PATH
    if ! command -v go &> /dev/null; then
        echo "⚠️  Go is not in PATH. Please run: export PATH=\$PATH:/usr/local/go/bin"
        echo "   Or restart your shell to pick up the changes."
        exit 1
    fi
    
    # Install Go dependencies
    echo "📦 Installing Go module dependencies..."
    go mod vendor
    
    echo ""
    echo "✅ Bootstrap complete! You can now run:"
    echo "   just build    - Build the asimi binary"
    echo "   just test     - Run the test suite"
    echo "   just run      - Run asimi in development mode"

run *args: modules
    go run . {{ quote(args) }}

modules:
    go mod vendor

# run the tests
test: modules
    go test -v ./...

build: modules
    go build -o asimi .

dlv:
    go install github.com/go-delve/delve/cmd/dlv@latest

debug: dlv
    dlv --listen=:2345 --headless=true --api-version=2 --accept-multiclient exec ./asimi -- --config config/default.toml
