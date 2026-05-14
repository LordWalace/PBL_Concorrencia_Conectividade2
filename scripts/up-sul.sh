#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

awk -v start="# --- SUL ---" -v end="# --- " '
  $0 == start {in=1; next}
  in && $0 ~ end {exit}
  in {sub(/^# ?/, ""); print}
' .env.example > .env

docker compose up -d --build gateway-sul beacon-sul
