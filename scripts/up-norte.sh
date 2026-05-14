#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ ! -f .env ]]; then
  awk -v start="# --- NORTE ---" -v end="# --- " '
    $0 == start {inside=1; next}
    inside && $0 ~ end {exit}
    inside {sub(/^# ?/, ""); print}
  ' .env.example > .env
  echo "Created .env for Norte"
else
  echo ".env already exists, preserving current configuration"
fi

docker compose up -d --build gateway-norte beacon-norte
