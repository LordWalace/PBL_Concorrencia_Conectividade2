#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

sector=""
if [[ $# -ge 1 ]]; then
  sector="$1"
else
  if [[ -f .env ]]; then
    sector=$(grep -E '^(GATEWAY_ID|SETOR_ID)=' .env | head -n1 | cut -d'=' -f2 | tr -d '[:space:]')
  fi
fi

if [[ -z "$sector" ]]; then
  echo "Uso: $0 [Norte|Sul|Leste|Oeste]"
  echo "Ou deixe .env com GATEWAY_ID/SETOR_ID definido e rode sem argumentos."
  exit 1
fi

sector=$(echo "$sector" | tr '[:upper:]' '[:lower:]')
case "$sector" in
  norte)
    drone=drone-norte-01
    ;;
  sul)
    drone=drone-sul-01
    ;;
  leste)
    drone=drone-leste-01
    ;;
  oeste)
    drone=drone-oeste-01
    ;;
  *)
    echo "Setor inválido: $sector"
    exit 1
    ;;
esac

echo "Subindo drone para o setor: $sector -> $drone"
docker compose up -d --build "$drone"
