#!/usr/bin/env bash
# Build the static binary and install it as a socket-activated systemd user service.
set -euo pipefail

cd "$(dirname "$0")"

BIN="$HOME/.local/bin/pushup-counter"
UNITS="$HOME/.config/systemd/user"

echo "==> building"
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o pushup-counter .

echo "==> installing"
install -D -m 0755 ./pushup-counter "$BIN"
install -d -m 0700 "$UNITS"
install -m 0644 deploy/pushup-counter.socket "$UNITS/pushup-counter.socket"
install -m 0644 deploy/pushup-counter.service "$UNITS/pushup-counter.service"

echo "==> enabling linger (survives logout and reboot)"
if [ "$(loginctl show-user "$USER" --property=Linger --value)" != "yes" ]; then
  sudo loginctl enable-linger "$USER"
fi
loginctl show-user "$USER" --property=Linger --value

echo "==> starting"
systemctl --user daemon-reload
systemctl --user enable --now pushup-counter.socket
# Restart the service if it was already running an older binary.
systemctl --user try-restart pushup-counter.service

echo "==> verifying"
systemctl --user is-enabled pushup-counter.socket
curl -fsS http://127.0.0.1:5554/healthz

cat <<'DONE'

Ready on http://localhost:5554

The service is socket activated: it is not enabled itself, and the first
connection to port 5554 starts it. Database: ~/.local/state/pushup-counter/pushups.db
DONE
