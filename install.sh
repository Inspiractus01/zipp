#!/bin/bash
set -e

REPO="Inspiractus01/zipp"
INSTALL_DIR="/usr/local/bin"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case $ARCH in
  x86_64)          ARCH="amd64" ;;
  arm64|aarch64)   ARCH="arm64" ;;
  *) echo "unsupported arch: $ARCH"; exit 1 ;;
esac

URL="https://github.com/$REPO/releases/latest/download/zipp-${OS}-${ARCH}"

echo "🪰 installing Zipp..."
echo "   platform: ${OS}/${ARCH}"

TMP=$(mktemp)
curl -sL --fail "$URL" -o "$TMP" || {
  echo "download failed — check https://github.com/$REPO/releases"
  exit 1
}
chmod +x "$TMP"
sudo mv "$TMP" "$INSTALL_DIR/zipp"

echo "✓ installed to $INSTALL_DIR/zipp"
echo "  run 'zipp --version' to verify"

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
