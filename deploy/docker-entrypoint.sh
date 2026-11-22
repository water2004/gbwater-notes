#!/bin/sh
set -eu

if [ "${1:-}" = gbwater ]; then
  node /app/web/scripts/prepare-frontend.mjs
fi

exec "$@"
