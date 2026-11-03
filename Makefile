.PHONY: all build-host build-client dist-windows dist-mac generate-icons test clean

VERSION ?= 1.0.0
BUILD_FLAGS = -ldflags="-s -w"

all: test dist-windows dist-mac

test:
	go test -v ./...

generate-icons:
	cd tools/genicons && go run . -source ../../internal/ui/icons -out ../../internal/ui/icons

# Cross-compile pure Go binary for Windows AMD64 (no CGO, no console window)
dist-windows:
	@mkdir -p dist/windows
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w -H windowsgui" -o dist/windows/bridge.exe ./cmd/bridge
	@cp -f scripts/install.ps1 dist/windows/
	@cp -f internal/ui/icons/cable-blue.ico dist/windows/lanbridge.ico
	@echo "Built dist/windows/bridge.exe successfully."

# Build native macOS ARM64 bundle with CGO (for systray menu bar)
dist-mac:
	@mkdir -p "dist/mac/LAN Bridge.app/Contents/MacOS"
	@mkdir -p "dist/mac/LAN Bridge.app/Contents/Resources"
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build $(BUILD_FLAGS) -o "dist/mac/LAN Bridge.app/Contents/MacOS/bridge" ./cmd/bridge
	@cp -f packaging/mac/Info.plist "dist/mac/LAN Bridge.app/Contents/Info.plist"
	@cp -f internal/ui/icons/AppIcon.icns "dist/mac/LAN Bridge.app/Contents/Resources/AppIcon.icns"
	@mkdir -p dist/mac/bin
	@cp -f "dist/mac/LAN Bridge.app/Contents/MacOS/bridge" dist/mac/bin/bridge
	@cp -f scripts/install.sh dist/mac/
	@cp -f docs/CODEX_SETUP.md docs/codex-config.toml.example dist/mac/
	@chmod +x dist/mac/install.sh
	@echo "Built dist/mac/LAN Bridge.app successfully."

clean:
	rm -rf dist
