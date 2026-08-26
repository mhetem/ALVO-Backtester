#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

if [ -f .env ]; then
	set -a
	# shellcheck disable=SC1091
	. ./.env
	set +a
fi

: "${BACKUP_REMOTE:?set BACKUP_REMOTE in .env, as in r2:alvo-backups}"
keep_days="${BACKUP_KEEP_DAYS:-30}"
floor_bytes="${BACKUP_MIN_BYTES:-1000000}"

name="alvo-$(date -u +%Y%m%dT%H%M%SZ).dump.gz"
workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

docker compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" -Fc "$POSTGRES_DB"' \
	| gzip >"$workdir/$name"

size="$(stat -c %s "$workdir/$name")"
if [ "$size" -lt "$floor_bytes" ]; then
	echo "refusing to upload $name: $size bytes is below the $floor_bytes floor" >&2
	echo "a dump this small means pg_dump failed or the database is empty" >&2
	exit 1
fi

rclone copyto "$workdir/$name" "$BACKUP_REMOTE/$name"
rclone delete "$BACKUP_REMOTE" --min-age "${keep_days}d"

echo "uploaded $name ($size bytes)"
