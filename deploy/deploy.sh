#!/bin/sh
# Pulls main and rebuilds the stack. Run on the VPS by the GitHub Actions
# deploy job (forced command of its SSH key) or by hand.
set -eu
cd "$(dirname "$0")/.."
git pull --ff-only
docker compose -f compose.prod.yml up -d --build
docker image prune -f
