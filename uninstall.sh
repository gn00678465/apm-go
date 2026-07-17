#!/bin/sh
set -e

# apm-go uninstaller (Linux / macOS)
#
# Removes ~/.local/bin/apm-go. The PATH line in ~/.profile is left
# untouched because ~/.local/bin is a shared, general-purpose
# directory. Idempotent: safe to run when apm-go is not installed.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/gn00678465/apm-go/main/uninstall.sh | sh

GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

INSTALL_DIR="$HOME/.local/bin"
BINARY_NAME="apm-go"
BINARY_PATH="$INSTALL_DIR/$BINARY_NAME"

if [ -e "$BINARY_PATH" ]; then
    rm -f "$BINARY_PATH"
    printf "%bRemoved %s%b\n" "$BLUE" "$BINARY_PATH" "$NC"
    printf "%bapm-go uninstalled.%b\n" "$GREEN" "$NC"
    printf "%bNote: ~/.profile is left untouched (~/.local/bin is shared with other tools).%b\n" "$BLUE" "$NC"
else
    printf "%bapm-go is not installed; nothing to do.%b\n" "$BLUE" "$NC"
fi
