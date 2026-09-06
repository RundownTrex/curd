#!/data/data/com.termux/files/usr/bin/bash
set -euo pipefail

REPO="${REPO:-RundownTrex/curd}"
ASSET="${ASSET:-curd-android-arm64}"
INSTALL_DIR="${PREFIX:-/data/data/com.termux/files/usr}/bin"
TARGET="${INSTALL_DIR}/curd"
TMP="$(mktemp)"

# Ensure am command is present to launch mpv-android
if ! command -v am >/dev/null 2>&1 && ! command -v termux-am >/dev/null 2>&1; then
    echo "Installing termux-am (required for player intents)..."
    pkg install -y termux-am || true
fi

echo "Downloading latest ${ASSET} from ${REPO}..."
if ! curl -fsSL "https://github.com/${REPO}/releases/download/latest/${ASSET}" -o "${TMP}" 2>/dev/null; then
    curl -fsSL "https://github.com/${REPO}/releases/latest/download/${ASSET}" -o "${TMP}"
fi
chmod +x "${TMP}"
mkdir -p "${INSTALL_DIR}"
mv "${TMP}" "${TARGET}"

echo "Successfully installed curd to ${TARGET}"
echo "Prerequisites:"
echo "  1. Install mpv-android (is.xyz.mpv) from F-Droid or Play Store."
echo "  2. Ensure Termux has 'Display over other apps' permission enabled."
echo "Run 'curd' to start watching."
