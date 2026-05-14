#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

PROJECT_NAME="pbl_concorrencia_conectividade2"

echo "Stopping and removing compose services (project=${PROJECT_NAME})..."
docker compose down --rmi all --volumes --remove-orphans || true

echo "Removing stale project containers..."
container_ids=$(docker ps -a --format '{{.ID}}\t{{.Names}}' | awk -F'\t' '$2 ~ /^(gateway|beacon|drone|device|client)(-|_|$)/ {print $1}')
if [[ -n "$container_ids" ]]; then
  echo "$container_ids" | xargs -r docker rm -f
  echo "Removed containers:"
  echo "$container_ids"
else
  echo "No stale project containers found."
fi

echo "Removing project networks..."
network_ids=$(docker network ls --format '{{.ID}}\t{{.Name}}' | awk -F'\t' '$2 ~ /^'"${PROJECT_NAME}"'/' | cut -f1)
if [[ -n "$network_ids" ]]; then
  echo "$network_ids" | xargs -r docker network rm
  echo "Removed networks:"
  echo "$network_ids"
else
  echo "No project networks found."
fi

echo "Removing project images..."
image_ids=$(docker images --format '{{.Repository}}:{{.Tag}}\t{{.ID}}' | awk -F'\t' '$1 ~ /^'"${PROJECT_NAME}"'(-|_)/ {print $2}')
if [[ -n "$image_ids" ]]; then
  echo "$image_ids" | xargs -r docker rmi -f
  echo "Removed images:"
  echo "$image_ids"
else
  echo "No project images found."
fi

echo "Cleanup complete."
