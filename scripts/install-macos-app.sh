#!/bin/sh
# Installs Claude Bridge.app into /Applications and moves this repo's config
# (.env, projects.json) to ~/Library/Application Support/ClaudeBridge if none is there yet.
set -eu
cd "$(dirname "$0")/.."

./scripts/build-macos-app.sh

DEST="/Applications/Claude Bridge.app"
SUPPORT="$HOME/Library/Application Support/ClaudeBridge"

osascript -e 'tell application "Claude Bridge" to quit' >/dev/null 2>&1 || true
rm -rf "$DEST"
cp -R "dist/Claude Bridge.app" "$DEST"

mkdir -p "$SUPPORT"
for f in .env projects.json; do
	if [ ! -f "$SUPPORT/$f" ]; then
		case "$f" in .env) example=.env.example ;; *) example=projects.example.json ;; esac
		if [ -f "$f" ]; then src="$f"; else src="$example"; fi
		cp "$src" "$SUPPORT/$f"
		echo "Config: copied $src -> $SUPPORT/$f"
	fi
done
chmod 600 "$SUPPORT/.env"

echo "Installed $DEST"
echo "Config folder: $SUPPORT"
