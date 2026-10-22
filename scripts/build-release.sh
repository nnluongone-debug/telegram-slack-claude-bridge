#!/bin/sh
# Builds release archives into dist/release:
#   macOS (Apple silicon): Claude Bridge.app (menu bar) + the claude-bridge CLI
#   Linux amd64/arm64 and Windows amd64/arm64: the claude-bridge CLI
# Run on a Mac (the menu bar app needs Xcode's clang). VERSION=1.2.3 sets the version.
set -eu
cd "$(dirname "$0")/.."

VERSION="${VERSION:-1.0.0}"
OUT="dist/release"
LDFLAGS="-s -w"
rm -rf "$OUT"
mkdir -p "$OUT"

# The Windows .exe gets the app icon and version info through a .syso resource
# file that `go build` links in automatically.
go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64,arm64 \
	--icon assets/icon.ico --manifest cli --product-name "Claude Bridge" \
	--file-description "Drive Claude Code from Slack and Telegram" \
	--product-version "$VERSION" --file-version "$VERSION" --out cmd/claude-bridge/rsrc
trap 'rm -f cmd/claude-bridge/rsrc_windows_*.syso' EXIT

cli() { # cli <goos> <goarch>: builds and archives the CLI
	name="claude-bridge_${VERSION}_$1_$2"
	dir="$OUT/$name"
	exe="claude-bridge"
	[ "$1" = windows ] && exe="claude-bridge.exe"
	mkdir -p "$dir"
	CGO_ENABLED=0 GOOS="$1" GOARCH="$2" go build -trimpath -ldflags "$LDFLAGS" -o "$dir/$exe" ./cmd/claude-bridge
	[ "$1" = darwin ] && codesign --force --sign - "$dir/$exe"
	cp README.md LICENSE .env.example projects.example.json "$dir/"
	if [ "$1" = windows ]; then
		(cd "$OUT" && zip -qr "$name.zip" "$name")
	else
		tar -C "$OUT" -czf "$OUT/$name.tar.gz" "$name"
	fi
	rm -rf "$dir"
	echo "Built $OUT/$name"
}

cli darwin arm64
cli linux amd64
cli linux arm64
cli windows amd64
cli windows arm64

ARCH=arm64 VERSION="$VERSION" ./scripts/build-macos-app.sh
(cd dist && ditto -c -k --keepParent "Claude Bridge.app" "release/Claude-Bridge_${VERSION}_macos_arm64.zip")
echo "Built $OUT/Claude-Bridge_${VERSION}_macos_arm64.zip"

(cd "$OUT" && shasum -a 256 ./*.zip ./*.tar.gz > SHA256SUMS)
ls -lh "$OUT"
