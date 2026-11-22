#!/bin/sh
set -eu

if [ "${1:-}" = export-frontend ]; then
  if [ "${GBWATER_BASE_PATH:-/mypdf/}" != "$(cat /opt/gbwater/frontend-base)" ]; then
    echo '镜像内的静态站挂载路径与 GBWATER_BASE_PATH 不一致。' >&2
    exit 1
  fi
  if [ -n "$(ls -A /app/web/dist)" ]; then
    echo '静态目录已有文件，保留当前站点。请使用空目录导出。' >&2
    exit 1
  fi
  cp -R /opt/gbwater/frontend/. /app/web/dist/
  exit 0
fi

exec "$@"
