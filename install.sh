#!/bin/bash
set -e

REPO="Inspiractus01/zipp"
BIN="zipp"
INSTALL_DIR="/usr/local/bin"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case $ARCH in
  x86_64)          ARCH="amd64" ;;
  arm64|aarch64)   ARCH="arm64" ;;
  *) echo "unsupported arch: $ARCH"; exit 1 ;;
esac

ASSET="${BIN}-${OS}-${ARCH}"
BASE="https://github.com/$REPO/releases/latest/download"

echo "🪰 installing Zipp..."
echo "   platform: ${OS}/${ARCH}"

TMPDL=$(mktemp -d)
trap 'rm -rf "$TMPDL"' EXIT

curl -L --fail --progress-bar "$BASE/$ASSET" -o "$TMPDL/$ASSET" || {
  echo "download failed — check https://github.com/$REPO/releases"
  exit 1
}

# verify checksum when the release ships one
if curl -sL --fail "$BASE/checksums.txt" -o "$TMPDL/checksums.txt" 2>/dev/null; then
  EXPECTED=$(awk -v a="$ASSET" '$2 == a {print $1}' "$TMPDL/checksums.txt")
  if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL=$(sha256sum "$TMPDL/$ASSET" | awk '{print $1}')
  else
    ACTUAL=$(shasum -a 256 "$TMPDL/$ASSET" | awk '{print $1}')
  fi
  if [ -z "$EXPECTED" ] || [ "$EXPECTED" != "$ACTUAL" ]; then
    echo "✗ checksum verification FAILED — refusing to install"
    echo "  expected: ${EXPECTED:-<missing>}"
    echo "  actual:   $ACTUAL"
    exit 1
  fi
  echo "✓ checksum verified"
else
  echo "! release has no checksums.txt — skipping verification"
fi

chmod +x "$TMPDL/$ASSET"
sudo mv "$TMPDL/$ASSET" "$INSTALL_DIR/$BIN"

echo "✓ installed to $INSTALL_DIR/$BIN"
echo "  run '$BIN --version' to verify"

# systemd on linux
if [ "$OS" = "linux" ] && command -v systemctl &>/dev/null; then
  echo ""
  echo "setting up systemd timer (runs every hour)..."

  sudo tee /etc/systemd/system/zipp.service > /dev/null << EOF
[Unit]
Description=Zipp backup runner

[Service]
Type=oneshot
ExecStart=$INSTALL_DIR/zipp run
User=$USER
EOF

  sudo tee /etc/systemd/system/zipp.timer > /dev/null << EOF
[Unit]
Description=Zipp backup timer

[Timer]
OnBootSec=5min
OnUnitActiveSec=1h
Persistent=true

[Install]
WantedBy=timers.target
EOF

  sudo systemctl daemon-reload
  sudo systemctl enable --now zipp.timer
  echo "✓ systemd timer enabled"
fi

echo ""
echo "🪰 done! run 'zipp' to open the UI"
