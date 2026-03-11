#!/bin/bash
set -e

REPO="inspiractus01/zipp"
BIN_NAME="zipp"
INSTALL_DIR="/usr/local/bin"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case $ARCH in
  x86_64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "unsupported arch: $ARCH"; exit 1 ;;
esac

LATEST=$(curl -sf "https://raw.githubusercontent.com/$REPO/main/latest.txt" || echo "")
if [ -z "$LATEST" ]; then
  echo "could not fetch latest version"
  exit 1
fi

URL="https://github.com/$REPO/releases/download/$LATEST/zipp-${OS}-${ARCH}"
TMP=$(mktemp)

echo "🪰 installing Zipp $LATEST..."
curl -sL "$URL" -o "$TMP"
chmod +x "$TMP"
sudo mv "$TMP" "$INSTALL_DIR/$BIN_NAME"

echo "✓ installed to $INSTALL_DIR/$BIN_NAME"

# systemd on linux
if [ "$OS" = "linux" ] && command -v systemctl &>/dev/null; then
  echo "setting up systemd timer..."

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
  echo "✓ systemd timer enabled (runs every hour)"
fi

echo ""
echo "run 'zipp' to open the UI"
