#!/bin/bash
set -euo pipefail
umask 077
cd /opt/podhod
install -d -m 700 backups
stage="$(mktemp -d /opt/podhod/backups/.partial.XXXXXX)"
trap 'rm -rf "$stage"' EXIT
for database in podhod keycloak; do
    docker compose exec -T db pg_dump -U postgres --format=custom "$database" > "$stage/$database.dump"
    docker compose exec -T db pg_restore --list < "$stage/$database.dump" > /dev/null
done
(
    cd "$stage"
    sha256sum podhod.dump keycloak.dump > SHA256SUMS
)
destination="/opt/podhod/backups/$(date -u +%Y%m%dT%H%M%SZ)-$$"
mv "$stage" "$destination"
trap - EXIT
printf 'Backup completed: %s\n' "$destination"
