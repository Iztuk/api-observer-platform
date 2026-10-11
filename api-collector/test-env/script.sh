#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir"

target_url="${TARGET_URL:-http://localhost:8082}"
duration_seconds="${DURATION_SECONDS:-60}"
request_delay_seconds="${REQUEST_DELAY_SECONDS:-0.25}"
wait_timeout_seconds="${WAIT_TIMEOUT_SECONDS:-120}"
run_once=false
manage_compose=true
keep_running=false

usage() {
    cat <<USAGE
Usage: ./script.sh [options]

Generate normal and attack traffic against the local ModSecurity proxy for
the OWASP Juice Shop test environment.

Options:
  --once                 Run one pass of normal and attack requests.
  --duration SECONDS     Run for this many seconds. Default: ${duration_seconds}
  --delay SECONDS        Delay between requests. Default: ${request_delay_seconds}
  --wait-timeout SECONDS Give up waiting for the proxy after this many seconds.
                         Default: ${wait_timeout_seconds}
  --url URL              Target URL. Default: ${target_url}
  --no-compose           Do not start or stop podman compose services.
  --keep-running         Leave compose services running when the script exits.
  -h, --help             Show this help.

Environment variables:
  TARGET_URL             Same as --url.
  DURATION_SECONDS       Same as --duration.
  REQUEST_DELAY_SECONDS  Same as --delay.
  WAIT_TIMEOUT_SECONDS   Same as --wait-timeout.
USAGE
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --once)
            run_once=true
            shift
            ;;
        --duration)
            duration_seconds="$2"
            shift 2
            ;;
        --delay)
            request_delay_seconds="$2"
            shift 2
            ;;
        --wait-timeout)
            wait_timeout_seconds="$2"
            shift 2
            ;;
        --url)
            target_url="$2"
            shift 2
            ;;
        --no-compose)
            manage_compose=false
            shift
            ;;
        --keep-running)
            keep_running=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $1" >&2
            usage >&2
            exit 1
            ;;
    esac
done

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "Required command not found: $1" >&2
        exit 1
    fi
}

request() {
    local label="$1"
    local method="$2"
    local path="$3"
    shift 3

    local status
    status="$(
        curl \
            -sS \
            -o /dev/null \
            -w "%{http_code}" \
            -X "$method" \
            "$target_url$path" \
            "$@" \
        || true
    )"

    printf "%-8s %-4s %-55s -> %s\n" "$label" "$method" "$path" "$status"
    sleep "$request_delay_seconds"
}

wait_for_proxy() {
    echo "Waiting for ModSecurity proxy at $target_url ..."

    local status
    local end_at=$((SECONDS + wait_timeout_seconds))

    until [[ "${status:-}" == "200" ]]; do
        if (( SECONDS >= end_at )); then
            echo "Timed out waiting for $target_url after ${wait_timeout_seconds}s. Last status: ${status:-none}" >&2
            exit 1
        fi

        status="$(
            curl \
                -s \
                -o /dev/null \
                -w "%{http_code}" \
                "$target_url/rest/products/search?q=ready" \
            || true
        )"

        sleep 1
    done

    echo "Proxy is ready"
}

cleanup() {
    if [[ "$manage_compose" == true && "$keep_running" == false ]]; then
        echo "Stopping compose services..."
        podman compose down
    fi
}

normal_traffic() {
    request normal GET "/"
    request normal GET "/#/search"
    request normal GET "/rest/products/search?q=apple"
    request normal GET "/rest/products/search?q=juice"
    request normal GET "/api/Challenges/"
    request normal GET "/api/BasketItems/"
    request normal POST "/api/Feedbacks/" \
        -H "Content-Type: application/json" \
        --data '{"UserId":1,"captchaId":0,"captcha":"0","comment":"Great shop experience","rating":4}'
    request normal POST "/rest/user/login" \
        -H "Content-Type: application/json" \
        --data '{"email":"guest@example.com","password":"not-a-real-password"}'
}

attack_traffic() {
    request attack GET "/rest/products/search?q=%27%20OR%201%3D1--"
    request attack GET "/rest/products/search?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E"
    request attack GET "/ftp/..%2F..%2F..%2F..%2Fetc%2Fpasswd"
    request attack GET "/?file=../../../../etc/passwd"
    request attack GET "/?cmd=cat%20/etc/passwd%3B%20id"
    request attack GET "/rest/products/search?q=%24%7Bjndi%3Aldap%3A%2F%2F127.0.0.1%2Fa%7D"
    request attack POST "/rest/user/login" \
        -H "Content-Type: application/json" \
        --data '{"email":"admin@juice-sh.op'\'' OR 1=1--","password":"anything"}'
    request attack POST "/api/Feedbacks/" \
        -H "Content-Type: application/json" \
        --data '{"UserId":1,"captchaId":0,"captcha":"0","comment":"<img src=x onerror=alert(1)>","rating":1}'
    request attack GET "/" \
        -H "User-Agent: sqlmap/1.7 test traffic" \
        -H "X-Forwarded-For: 127.0.0.1'\'' OR '\''1'\''='\''1"
}

run_pass() {
    normal_traffic
    attack_traffic
}

require_command curl

if [[ "$manage_compose" == true ]]; then
    require_command podman
    echo "Starting compose services..."
    podman compose up -d
    trap cleanup EXIT INT TERM
fi

wait_for_proxy

echo "Generating traffic..."

if [[ "$run_once" == true ]]; then
    run_pass
else
    end_at=$((SECONDS + duration_seconds))

    while (( SECONDS < end_at )); do
        run_pass
    done
fi

echo "Traffic generation complete"
