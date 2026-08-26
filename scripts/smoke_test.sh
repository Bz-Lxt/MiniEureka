#!/bin/sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_root"

docker compose config --quiet
docker compose up --build -d

ui_port=${MINIEUREKA_UI_PORT:-18780}
node1_port=${MINIEUREKA_NODE1_PORT:-18781}
node2_port=${MINIEUREKA_NODE2_PORT:-18782}
node3_port=${MINIEUREKA_NODE3_PORT:-18783}
export MINIEUREKA_BASE_URL="http://127.0.0.1:${ui_port}"
export MINIEUREKA_NODE_URLS="http://127.0.0.1:${node1_port},http://127.0.0.1:${node2_port},http://127.0.0.1:${node3_port}"

attempt=0
until curl --max-time 2 --fail --silent --show-error \
  "${MINIEUREKA_BASE_URL}/readyz" >/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 45 ]; then
    docker compose ps
    docker compose logs --tail=120
    echo "Mini Eureka did not become ready" >&2
    exit 1
  fi
  sleep 2
done

pytest -q tests/api_smoke.py

if [ -f frontend-admin/playwright.config.ts ] && [ -f frontend-admin/e2e/e2e_flow.spec.ts ]; then
  (
    cd frontend-admin
    npx playwright test e2e/e2e_flow.spec.ts
  )
fi

docker compose ps
echo "Smoke test passed. Services remain running; use 'docker compose down' when finished."
