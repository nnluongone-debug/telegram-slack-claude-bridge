#!/bin/sh
# Builds dist/Claude Bridge.app: a menu-bar-only app (no Dock icon) wrapping the bridge.
# ARCH=arm64 (Apple silicon) or amd64 (Intel); defaults to this Mac's architecture.
set -eu
cd "$(dirname "$0")/.."

APP="dist/Claude Bridge.app"
VERSION="${VERSION:-1.0.0}"
ARCH="${ARCH:-$(go env GOARCH)}"
BUNDLE_ID="com.nnluong.claude-bridge"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
# The menu bar library uses Cocoa through cgo; clang cross-compiles between Intel and Apple silicon.
CGO_ENABLED=1 GOOS=darwin GOARCH="$ARCH" go build -trimpath -ldflags "-s -w" \
	-o "$APP/Contents/MacOS/claude-bridge" ./cmd/claude-bridge-menubar

# App icon: every size iconutil expects, from the 1024-px master.
ICONSET="$(mktemp -d)/AppIcon.iconset"
mkdir -p "$ICONSET"
for size in 16 32 128 256 512; do
	sips -z "$size" "$size" assets/icon-1024.png --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
	double=$((size * 2))
	sips -z "$double" "$double" assets/icon-1024.png --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"
rm -rf "$(dirname "$ICONSET")"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>Claude Bridge</string>
	<key>CFBundleDisplayName</key><string>Claude Bridge</string>
	<key>CFBundleIdentifier</key><string>${BUNDLE_ID}</string>
	<key>CFBundleExecutable</key><string>claude-bridge</string>
	<key>CFBundleIconFile</key><string>AppIcon</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>${VERSION}</string>
	<key>CFBundleVersion</key><string>${VERSION}</string>
	<key>LSMinimumSystemVersion</key><string>11.0</string>
	<key>LSUIElement</key><true/>
	<key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

# Ad-hoc signature: stable identity for macOS privacy permissions (Documents access, etc.).
# Apple silicon refuses to run unsigned binaries, so this is required there.
codesign --force --deep --sign - "$APP"
echo "Built $APP ($ARCH)"
