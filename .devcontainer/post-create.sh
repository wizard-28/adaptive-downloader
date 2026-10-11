#!/usr/bin/env bash

set -euo pipefail

echo "=== Installing system dependencies ==="

sudo apt-get update

sudo apt-get install -y \
  aria2 \
  iproute2 \
  iputils-ping \
  iperf3 \
  tcpdump \
  jq \
  dnsutils \
  make \
  autoconf \
  automake \
  libtool \
  iptables \
  dnsmasq \
  apache2 \
  apache2-dev \
  protobuf-compiler \
  pkg-config \
  libssl-dev \
  libxcb-present-dev \
  libpangomm-2.48-dev

echo
echo "=== Building Mahimahi ==="

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
MAHIMAHI_DIR="${WORKSPACE_DIR}/mahimahi-clone-dir/mahimahi"

if [[ ! -d "${MAHIMAHI_DIR}" ]]; then
  echo "ERROR: Mahimahi source directory not found:"
  echo "       ${MAHIMAHI_DIR}"
  exit 1
fi

cd "${MAHIMAHI_DIR}"

echo "Mahimahi source: ${MAHIMAHI_DIR}"

./autogen.sh
./configure
make -j"$(nproc)"
sudo make install
sudo ldconfig

echo
echo "=== Tool versions ==="

go version
python3 --version
aria2c --version | head -n 1
tc -V
iperf3 --version | head -n 1
opencode --version

echo
echo "=== Mahimahi ==="

echo
echo "=== Mahimahi traces ==="

INSTALLED_TRACES="/usr/local/share/mahimahi/traces"

if [[ -d "${INSTALLED_TRACES}" ]]; then
  find "${INSTALLED_TRACES}" -maxdepth 1 -type f \
    -printf '%f\n' | sort | head -20
else
  echo "No installed Mahimahi trace directory found."
fi

echo "=== mm-link runtime smoke test ==="
if timeout 15 mm-link "${INSTALLED_TRACES}/Verizon-LTE-short.up" "${INSTALLED_TRACES}/Verizon-LTE-short.down" -- bash -c 'echo mm-link namespace OK'; then
  echo "mm-link smoke test passed."
else
  echo "WARNING: mm-link failed to create a working shell — check seccomp/capabilities."
fi

echo
echo "=== Container ready ==="o
