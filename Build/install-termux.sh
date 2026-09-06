#!/data/data/com.termux/files/usr/bin/bash
set -euo pipefail

REPO="${REPO:-RundownTrex/curd}"
ASSET="${ASSET:-curd-android-arm64}"
INSTALL_DIR="${PREFIX:-/data/data/com.termux/files/usr}/bin"
TARGET="${INSTALL_DIR}/curd"
TMP="$(mktemp)"

echo "Downloading latest ${ASSET} from ${REPO}..."
if ! curl -fsSL "https://github.com/${REPO}/releases/download/latest/${ASSET}" -o "${TMP}" 2>/dev/null; then
    curl -fsSL "https://github.com/${REPO}/releases/latest/download/${ASSET}" -o "${TMP}"
fi
chmod +x "${TMP}"
mkdir -p "${INSTALL_DIR}"
mv "${TMP}" "${TARGET}"

echo "Successfully installed curd to ${TARGET}"
echo "Run 'curd' to start watching."
