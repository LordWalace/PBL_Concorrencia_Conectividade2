#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

awk -v start="# --- SUL ---" -v end="# --- " '
  $0 == start {inside=1; next}
  inside && $0 ~ end {exit}
  inside {sub(/^# ?/, ""); print}
' .env.example > .env

docker compose up -d --build gateway-sul beacon-sul
