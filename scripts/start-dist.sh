#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
cd "$ROOT_DIR"

if [ ! -f .env ]; then
    echo "警告：当前目录没有 .env，将使用程序默认配置。"
fi

chmod +x bin/infinite-canvas-api
pm2 startOrReload ecosystem.config.cjs --update-env
pm2 save
pm2 status
