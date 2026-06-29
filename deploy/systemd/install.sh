#!/usr/bin/env bash
set -euo pipefail

# Installs fabriquilla as a systemd service on the lab host.
# Run as root or with sudo.

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
INSTALL_DIR=/usr/local/bin
CONFIG_DIR=/etc/fabriquilla
STATE_DIR=/var/lib/fabriquilla
POLICY_DIR="${CONFIG_DIR}/policies"

echo "==> Building binaries"
cd "$REPO_ROOT"
CGO_ENABLED=0 make build

echo "==> Installing binaries to ${INSTALL_DIR}"
cp bin/* "${INSTALL_DIR}/"

echo "==> Creating service user"
id -u fabriquilla &>/dev/null || useradd --system --no-create-home --shell /usr/sbin/nologin fabriquilla

echo "==> Creating directories"
mkdir -p "${CONFIG_DIR}/keys" "${STATE_DIR}" "${POLICY_DIR}"

echo "==> Installing sandbox policies"
cp deploy/sandbox-policies/*.yaml "${POLICY_DIR}/"

echo "==> Installing systemd unit"
cp deploy/systemd/fabriquilla.service /etc/systemd/system/
systemctl daemon-reload

echo ""
echo "Done. Before starting the service:"
echo ""
echo "  1. Copy your config:     cp config.json ${CONFIG_DIR}/config.json"
echo "  2. Copy PEM keys:        cp *.pem ${CONFIG_DIR}/keys/"
echo "  3. Create env file:      cp deploy/systemd/env.example ${CONFIG_DIR}/env"
echo "     Edit ${CONFIG_DIR}/env with real API keys"
echo "  4. Fix ownership:        chown -R fabriquilla:fabriquilla ${STATE_DIR}"
echo "     chown -R root:fabriquilla ${CONFIG_DIR}"
echo "     chmod 640 ${CONFIG_DIR}/keys/*.pem ${CONFIG_DIR}/env"
echo ""
echo "  Then: systemctl enable --now fabriquilla"
echo "  Logs: journalctl -u fabriquilla -f"
