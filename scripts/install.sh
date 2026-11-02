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

if [ ! -d "$APP_SOURCE" ]; then
    echo "LAN Bridge.app not found in package: $APP_SOURCE" >&2
    exit 1
fi
if [ ! -f "$BIN_SOURCE" ]; then
    echo "bridge MCP binary not found in package: $BIN_SOURCE" >&2
    exit 1
fi

mkdir -p "$HOME/Applications" "$HOME/.local/bin"
ditto "$APP_SOURCE" "$APP_DEST"
install -m 755 "$BIN_SOURCE" "$BIN_DEST"

echo "Installed LAN Bridge.app at: $APP_DEST"
echo "Installed bridge MCP command at: $BIN_DEST"
echo "Register Claude Code MCP with:"
printf '  claude mcp add lanbridge -- "%s" mcp\n' "$BIN_DEST"
echo "Opening the app; allow Local Network access when macOS asks."
open "$APP_DEST"
