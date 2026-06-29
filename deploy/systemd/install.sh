#!/usr/bin/env bash
set -euo pipefail

REPO="ruromero/la-fabriquilla"
VERSION="latest"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/fabriquilla"
STATE_DIR="/var/lib/fabriquilla"
POLICY_DIR="${CONFIG_DIR}/policies"

usage() {
  echo "Usage: install.sh [--version VERSION]"
  echo ""
  echo "Install fabriquilla as a systemd service."
  echo ""
  echo "  --version   Release tag to install (default: latest)"
  echo ""
  echo "Examples:"
  echo "  curl -sSL https://raw.githubusercontent.com/${REPO}/main/deploy/systemd/install.sh | sudo bash"
  echo "  curl -sSL ... | sudo bash -s -- --version v0.1.0"
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --help|-h) usage ;;
    *) echo "Unknown option: $1"; usage ;;
  esac
done

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "Error: this installer only supports Linux" >&2
  exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Error: run as root or with sudo" >&2
  exit 1
fi

ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64) ARCH="amd64" ;;
  *) echo "Error: unsupported architecture ${ARCH}" >&2; exit 1 ;;
esac

if [[ "${VERSION}" == "latest" ]]; then
  DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/fabriquilla-linux-${ARCH}.tar.gz"
else
  DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/fabriquilla-linux-${ARCH}.tar.gz"
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "${TMPDIR}"' EXIT

echo "==> Downloading ${DOWNLOAD_URL}"
curl -fsSL "${DOWNLOAD_URL}" -o "${TMPDIR}/fabriquilla.tar.gz"

echo "==> Extracting"
tar -xzf "${TMPDIR}/fabriquilla.tar.gz" -C "${TMPDIR}"

echo "==> Installing binaries to ${INSTALL_DIR}"
install -m 755 "${TMPDIR}"/fabriquilla/dispatcher "${INSTALL_DIR}/"
install -m 755 "${TMPDIR}"/fabriquilla/gatherer "${INSTALL_DIR}/"
install -m 755 "${TMPDIR}"/fabriquilla/researcher "${INSTALL_DIR}/"
install -m 755 "${TMPDIR}"/fabriquilla/planner "${INSTALL_DIR}/"
install -m 755 "${TMPDIR}"/fabriquilla/designer "${INSTALL_DIR}/"
install -m 755 "${TMPDIR}"/fabriquilla/coder "${INSTALL_DIR}/"
install -m 755 "${TMPDIR}"/fabriquilla/committer "${INSTALL_DIR}/"
install -m 755 "${TMPDIR}"/fabriquilla/reviewer "${INSTALL_DIR}/"
install -m 755 "${TMPDIR}"/fabriquilla/iterator "${INSTALL_DIR}/"
install -m 755 "${TMPDIR}"/fabriquilla/feedback "${INSTALL_DIR}/"

echo "==> Creating service user"
id -u fabriquilla &>/dev/null || useradd --system --no-create-home --shell /usr/sbin/nologin fabriquilla

echo "==> Creating directories"
mkdir -p "${CONFIG_DIR}/keys" "${STATE_DIR}" "${POLICY_DIR}"

echo "==> Installing sandbox policies"
install -m 644 "${TMPDIR}"/fabriquilla/policies/*.yaml "${POLICY_DIR}/"

echo "==> Installing systemd unit"
install -m 644 "${TMPDIR}/fabriquilla/fabriquilla.service" /etc/systemd/system/
systemctl daemon-reload

if [[ ! -f "${CONFIG_DIR}/env" ]]; then
  install -m 640 "${TMPDIR}/fabriquilla/env.example" "${CONFIG_DIR}/env"
  echo "==> Created ${CONFIG_DIR}/env from template — edit it with real API keys"
fi

chown -R fabriquilla:fabriquilla "${STATE_DIR}"

echo ""
echo "Installed successfully. Before starting the service:"
echo ""
echo "  1. Copy your config:  cp config.json ${CONFIG_DIR}/config.json"
echo "  2. Copy PEM keys:     cp *.pem ${CONFIG_DIR}/keys/"
echo "  3. Edit API keys:     vi ${CONFIG_DIR}/env"
echo "  4. Fix permissions:   chown root:fabriquilla ${CONFIG_DIR}/keys/*.pem"
echo "                        chmod 640 ${CONFIG_DIR}/keys/*.pem ${CONFIG_DIR}/env"
echo ""
echo "  Then: systemctl enable --now fabriquilla"
echo "  Logs: journalctl -u fabriquilla -f"
