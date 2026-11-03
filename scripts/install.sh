#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -d "$SCRIPT_DIR/LAN Bridge.app" ]; then
    PACKAGE_DIR=$SCRIPT_DIR
else
    PACKAGE_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../dist/mac" && pwd)
fi

APP_SOURCE="$PACKAGE_DIR/LAN Bridge.app"
BIN_SOURCE="$PACKAGE_DIR/bin/bridge"
APP_DEST="$HOME/Applications/LAN Bridge.app"
BIN_DEST="$HOME/.local/bin/bridge"
SETUP_SOURCE="$PACKAGE_DIR/CODEX_SETUP.md"
CODEX_CONFIG_SOURCE="$PACKAGE_DIR/codex-config.toml.example"

if [ ! -d "$APP_SOURCE" ]; then
    echo "LAN Bridge.app not found in package: $APP_SOURCE" >&2
    exit 1
fi
if [ ! -f "$BIN_SOURCE" ]; then
    echo "bridge MCP binary not found in package: $BIN_SOURCE" >&2
    exit 1
fi
if [ ! -f "$SETUP_SOURCE" ] || [ ! -f "$CODEX_CONFIG_SOURCE" ]; then
    echo "Agent MCP setup files are missing from package: $PACKAGE_DIR" >&2
    exit 1
fi

mkdir -p "$HOME/Applications" "$HOME/.local/bin"
ditto "$APP_SOURCE" "$APP_DEST"
install -m 755 "$BIN_SOURCE" "$BIN_DEST"

DESKTOP_SHORTCUT="$HOME/Desktop/LAN Bridge.app"
if [ -d "$HOME/Desktop" ] && [ ! -e "$DESKTOP_SHORTCUT" ] && [ ! -L "$DESKTOP_SHORTCUT" ]; then
    ln -s "$APP_DEST" "$DESKTOP_SHORTCUT"
fi

echo "Installed LAN Bridge.app at: $APP_DEST"
echo "Installed bridge MCP command at: $BIN_DEST"
echo "Open Bridge Settings to select Claude Code or Codex for MCP registration."
echo "Manual agent setup guide: $SETUP_SOURCE"
echo "Codex config example: $CODEX_CONFIG_SOURCE"
printf '  Claude Code: claude mcp add --scope user --transport stdio lanbridge -- "%s" mcp\n' "$APP_DEST/Contents/MacOS/bridge"
printf '  Codex: codex mcp add lanbridge -- "%s" mcp\n' "$APP_DEST/Contents/MacOS/bridge"
echo "Opening the app; allow Local Network access when macOS asks."
open "$APP_DEST"
