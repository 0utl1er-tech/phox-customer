#!/bin/sh

set -e

echo "Run db migration"
/usr/bin/migrate -path /root/migration -database "$DB_SOURCE" -verbose up

echo "Start the application"
exec "$@"
