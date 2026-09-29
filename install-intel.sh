#!/usr/bin/env bash
# ==============================================================================
# One-liner install for Intel Macs (x86_64) — a thin wrapper
# over install.sh, which holds the one real install procedure.
# ==============================================================================
set -euo pipefail
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install.sh | VPN_ARCH=amd64 bash
