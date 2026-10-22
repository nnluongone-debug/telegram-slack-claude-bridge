.PHONY: build run test smoke lint app install-app release icons

build:
	go build -o bin/claude-bridge ./cmd/claude-bridge

run: build
	./bin/claude-bridge

test:
	go test -race ./...

# Live test against the installed claude CLI (uses your Claude account)
smoke:
	go test -tags smoke -run TestClaudeCLILiveApproval -v ./internal/session/

lint:
	go vet ./... && test -z "$$(gofmt -l .)"

# macOS menu bar app: dist/Claude Bridge.app (ARCH=arm64|amd64, default: this Mac)
app:
	./scripts/build-macos-app.sh

# Build, copy to /Applications and set up the config folder
install-app:
	./scripts/install-macos-app.sh

# Release archives for macOS (Apple silicon), Linux and Windows in dist/release
release:
	./scripts/build-release.sh

# Redraw assets/ (needs Pillow: pip install pillow)
icons:
	python3 scripts/generate-icons.py
