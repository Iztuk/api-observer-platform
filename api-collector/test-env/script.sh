 #!/bin/bash

set -e

nginx_url="http://localhost:8080"

echo "Stopping existing containers..."
podman compose down

echo "Starting containers..."
podman compose up -d

echo "Waiting for nginx..."
until curl -s -o /dev/null "$nginx_url"; do
    sleep 1
done

echo "nginx is ready"

cleanup() {
    echo "Cleaning up..."

    podman compose down
}

trap cleanup EXIT INT TERM

echo "Generating traffic..."

while true; do
    curl -s -o /dev/null "$nginx_url"
done
