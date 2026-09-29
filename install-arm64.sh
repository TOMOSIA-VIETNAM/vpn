#!/usr/bin/env bash
# ==============================================================================
# One-liner install for Apple Silicon (ARM64: M1/M2/M3/M4) — a thin wrapper
# over install.sh, which holds the one real install procedure.
# ==============================================================================
set -euo pipefail
curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install.sh | VPN_ARCH=arm64 bash
