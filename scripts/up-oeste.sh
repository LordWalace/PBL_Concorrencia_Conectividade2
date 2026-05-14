#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ ! -f .env ]]; then
  awk -v start="# --- OESTE ---" -v end="# --- " '
    $0 == start {inside=1; next}
    inside && $0 ~ end {exit}
    inside {sub(/^# ?/, ""); print}
  ' .env.example > .env
  echo "Created .env for Oeste"
else
  echo ".env already exists, preserving current configuration"
fi

docker compose up -d --build gateway-oeste beacon-oeste
