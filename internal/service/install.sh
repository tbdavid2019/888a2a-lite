#!/usr/bin/env bash
# ==============================================================================
# 888a2a-lite Universal Agent Bridge One-Line Installer
# Usage:
#   curl -fsSL https://a2a.david888.com/install.sh | bash -s -- --name "MyAgent" --backend openclaw --install-service
# ==============================================================================
set -euo pipefail

HUB_URL="${A2A888_HUB_URL:-https://a2a.david888.com}"
INSTALL_DIR="${HOME}/.a2a/bin"
TARGET_BIN="${INSTALL_DIR}/a2a-bridge"
GITHUB_RAW="https://raw.githubusercontent.com/tbdavid2019/888a2a-lite/main/examples/worker/a2a_bridge.py"

echo "================================================================="
echo " 888a2a-lite Universal Agent Bridge Installer"
echo "================================================================="

# 1. Verify Python 3 (>= 3.10)
if ! command -v python3 >/dev/null 2>&1; then
  echo "[!] Error: python3 is required but not found in PATH." >&2
  echo "    Please install Python 3.10+ and retry." >&2
  exit 1
fi

PY_VER=$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')
PY_OK=$(python3 -c 'import sys; print("1" if sys.version_info >= (3, 10) else "0")')
if [ "$PY_OK" != "1" ]; then
  echo "[!] Error: Python 3.10+ required. Found Python $PY_VER." >&2
  exit 1
fi
echo "[✓] Detected Python $PY_VER"

# 2. Parse Hub URL if overridden in arguments
FORWARD_ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --hub)
      HUB_URL="$2"
      FORWARD_ARGS+=("$1" "$2")
      shift 2
      ;;
    --hub=*)
      HUB_URL="${1#*=}"
      FORWARD_ARGS+=("$1")
      shift
      ;;
    *)
      FORWARD_ARGS+=("$1")
      shift
      ;;
  esac
done

# Ensure HUB_URL is set in forwarded arguments if not explicitly provided
HAS_HUB=false
for arg in "${FORWARD_ARGS[@]}"; do
  if [[ "$arg" == "--hub" || "$arg" == --hub=* ]]; then
    HAS_HUB=true
    break
  fi
done
if [ "$HAS_HUB" = false ]; then
  FORWARD_ARGS+=("--hub" "$HUB_URL")
fi

mkdir -p "$INSTALL_DIR" "${HOME}/.a2a/logs"

# 3. Download standalone bridge
echo "[*] Downloading a2a-bridge from ${HUB_URL}/a2a_bridge.py..."
DOWNLOAD_SUCCESS=false

if curl -fsSL "${HUB_URL}/a2a_bridge.py" -o "$TARGET_BIN" 2>/dev/null; then
  if grep -q "Universal Agent Bridge" "$TARGET_BIN"; then
    DOWNLOAD_SUCCESS=true
  fi
fi

# Fallback to GitHub raw if hub endpoint not yet serving asset
if [ "$DOWNLOAD_SUCCESS" = false ]; then
  echo "[*] Hub asset route not ready, downloading from GitHub repository..."
  if curl -fsSL "$GITHUB_RAW" -o "$TARGET_BIN"; then
    DOWNLOAD_SUCCESS=true
  fi
fi

if [ "$DOWNLOAD_SUCCESS" = false ]; then
  echo "[!] Failed to download a2a_bridge.py. Please check network or Hub URL." >&2
  exit 1
fi

chmod +x "$TARGET_BIN"
echo "[✓] a2a-bridge installed at: $TARGET_BIN"

# 4. Optional symlink to /usr/local/bin if writable
if [ -w "/usr/local/bin" ] && [ ! -e "/usr/local/bin/a2a-bridge" ]; then
  ln -sf "$TARGET_BIN" /usr/local/bin/a2a-bridge 2>/dev/null || true
  echo "[✓] Symlinked to /usr/local/bin/a2a-bridge"
fi

# 5. Execute bridge with provided arguments
if [ ${#FORWARD_ARGS[@]} -gt 0 ]; then
  echo "[*] Launching a2a-bridge with arguments: ${FORWARD_ARGS[*]}"
  exec python3 "$TARGET_BIN" "${FORWARD_ARGS[@]}"
else
  echo ""
  echo "Installation complete! You can now run:"
  echo "  $TARGET_BIN --name \"MyAgent\" --backend openclaw --install-service"
  echo ""
  echo "Add ~/.a2a/bin to your PATH to run 'a2a-bridge' directly:"
  echo "  export PATH=\"\$HOME/.a2a/bin:\$PATH\""
fi
