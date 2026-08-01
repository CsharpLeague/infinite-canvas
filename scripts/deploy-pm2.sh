#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT_DIR"

mkdir -p bin
go build -o bin/infinite-canvas-api .

cd web
bun install --frozen-lockfile
bun run build
mkdir -p .next/standalone/.next
cp -R public .next/standalone/public
cp -R .next/static .next/standalone/.next/static

cd "$ROOT_DIR"
pm2 startOrReload ecosystem.config.cjs --update-env
pm2 save
