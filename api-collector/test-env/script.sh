#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir"

target_url_a="${TARGET_URL_A:-http://localhost:8082}"
target_url_b="${TARGET_URL_B:-http://localhost:8083}"
target_url_c="${TARGET_URL_C:-http://localhost:8084}"

request_delay_seconds="${REQUEST_DELAY_SECONDS:-0.25}"
request_jitter_seconds="${REQUEST_JITTER_SECONDS:-0.20}"
max_start_stagger_seconds="${MAX_START_STAGGER_SECONDS:-2.0}"
wait_timeout_seconds="${WAIT_TIMEOUT_SECONDS:-120}"

manage_compose=true
keep_running=false

worker_pids=()

usage() {
    cat <<USAGE
Usage: ./script.sh [options]

Generate continuous concurrent normal and attack traffic against three local
ModSecurity proxies.

Traffic continues until Ctrl+C is pressed.

Options:
  --delay SECONDS
      Base delay between requests.
      Default: ${request_delay_seconds}

  --jitter SECONDS
      Maximum +/- random jitter added to each request delay.
      Default: ${request_jitter_seconds}

  --stagger SECONDS
      Maximum random startup delay for each node.
      Default: ${max_start_stagger_seconds}

  --wait-timeout SECONDS
      Give up waiting for a proxy after this many seconds.
      Default: ${wait_timeout_seconds}

  --url-a URL
      Node A URL.
      Default: ${target_url_a}

  --url-b URL
      Node B URL.
      Default: ${target_url_b}

  --url-c URL
      Node C URL.
      Default: ${target_url_c}

  --no-compose
      Do not start or stop Podman Compose.

  --keep-running
      Leave Compose services running when the script exits.

  -h, --help
      Show this help.

Environment variables:
  TARGET_URL_A
  TARGET_URL_B
  TARGET_URL_C
  REQUEST_DELAY_SECONDS
  REQUEST_JITTER_SECONDS
  MAX_START_STAGGER_SECONDS
  WAIT_TIMEOUT_SECONDS
USAGE
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --delay)
            request_delay_seconds="$2"
            shift 2
            ;;

        --jitter)
            request_jitter_seconds="$2"
            shift 2
            ;;

        --stagger)
            max_start_stagger_seconds="$2"
            shift 2
            ;;

        --wait-timeout)
            wait_timeout_seconds="$2"
            shift 2
            ;;

        --url-a)
            target_url_a="$2"
            shift 2
            ;;

        --url-b)
            target_url_b="$2"
            shift 2
            ;;

        --url-c)
            target_url_c="$2"
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

random_delay() {
    awk \
        -v base="$request_delay_seconds" \
        -v jitter="$request_jitter_seconds" \
        -v random="$RANDOM" \
        'BEGIN {
            fraction = random / 32767.0
            delta = ((fraction * 2.0) - 1.0) * jitter
            delay = base + delta

            if (delay < 0) {
                delay = 0
            }

            printf "%.3f", delay
        }'
}

random_stagger() {
    awk \
        -v max="$max_start_stagger_seconds" \
        -v random="$RANDOM" \
        'BEGIN {
            if (max <= 0) {
                printf "0"
                exit
            }

            printf "%.3f", (random / 32767.0) * max
        }'
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
            --connect-timeout 2 \
            --max-time 5 \
            -o /dev/null \
            -w "%{http_code}" \
            -X "$method" \
            "$target_url$path" \
            "$@" \
        || true
    )"

    printf "[%-6s] %-8s %-7s %-55s -> %s\n" \
        "$node_name" \
        "$label" \
        "$method" \
        "$path" \
        "$status"

    sleep "$(random_delay)"
}

section() {
    printf "\n[%s] == %s ==\n" \
        "$node_name" \
        "$1"
}

wait_for_proxy() {
    local node_label="$1"
    local proxy_url="$2"

    echo "[$node_label] Waiting for ModSecurity proxy at $proxy_url ..."

    local status=""
    local end_at=$((SECONDS + wait_timeout_seconds))

    until [[ "$status" == "200" ]]; do
        if (( SECONDS >= end_at )); then
            echo "[$node_label] Timed out waiting for $proxy_url after ${wait_timeout_seconds}s. Last status: ${status:-none}" >&2
            return 1
        fi

        status="$(
            curl \
                -s \
                --connect-timeout 2 \
                --max-time 5 \
                -o /dev/null \
                -w "%{http_code}" \
                "$proxy_url/rest/products/search?q=ready" \
            || true
        )"

        sleep 1
    done

    echo "[$node_label] Proxy is ready"
}

normal_traffic() {
    section "normal browsing and API traffic"

    request normal GET "/"
    request normal GET "/#/search"
    request normal GET "/assets/public/images/JuiceShop_Logo.png"
    request normal GET "/rest/admin/application-configuration"
    request normal GET "/rest/products/search?q=apple"
    request normal GET "/rest/products/search?q=juice"
    request normal GET "/rest/products/search?q=banana"
    request normal GET "/rest/products/1/reviews"
    request normal GET "/api/Challenges/"
    request normal GET "/api/Quantitys/"
    request normal GET "/api/Cards/"
    request normal GET "/api/BasketItems/"
    request normal OPTIONS "/rest/products/search?q=apple"

    request normal POST "/api/Feedbacks/" \
        -H "Content-Type: application/json" \
        --data '{"UserId":1,"captchaId":0,"captcha":"0","comment":"Great shop experience","rating":4}'

    request normal POST "/rest/user/login" \
        -H "Content-Type: application/json" \
        --data '{"email":"guest@example.com","password":"not-a-real-password"}'
}

error_traffic() {
    section "ordinary error traffic"

    request error GET "/does-not-exist"
    request error HEAD "/does-not-exist"
    request error GET "/api/Products/999999"
    request error GET "/rest/products/search?q="

    request error POST "/rest/user/login" \
        -H "Content-Type: application/json" \
        --data '{"email":"","password":""}'

    request error POST "/rest/user/login" \
        -H "Content-Type: text/plain" \
        --data "not json"

    request error PUT "/api/BasketItems/999999" \
        -H "Content-Type: application/json" \
        --data '{"ProductId":1,"BasketId":1,"quantity":2}'

    request error DELETE "/api/BasketItems/999999"
}

sqli_traffic() {
    section "sqli probes"

    request sqli GET "/rest/products/search?q=%27%20OR%201%3D1--"
    request sqli GET "/rest/products/search?q=%27%20UNION%20SELECT%20NULL%2CNULL%2CNULL--"
    request sqli GET "/rest/products/search?q=%27%29%20OR%20%28%271%27%3D%271"
    request sqli GET "/rest/products/search?q=1%3BSELECT%20pg_sleep%285%29--"
    request sqli GET "/rest/products/search?q=1%27%20AND%20SLEEP%285%29--"

    request sqli POST "/rest/user/login" \
        -H "Content-Type: application/json" \
        --data '{"email":"admin@juice-sh.op'\'' OR 1=1--","password":"anything"}'

    request sqli POST "/api/Feedbacks/" \
        -H "Content-Type: application/json" \
        --data '{"UserId":"1 OR 1=1","captchaId":0,"captcha":"0","comment":"normal text","rating":5}'

    request sqli PUT "/api/Products/1" \
        -H "Content-Type: application/json" \
        --data '{"name":"Juice'\'' OR 1=1--","description":"sql test","price":1.99}'

    request sqli PATCH "/api/Users/1" \
        -H "Content-Type: application/json" \
        --data '{"email":"updated@example.com'\'' UNION SELECT password FROM Users--"}'

    request sqli DELETE "/api/BasketItems/1?id=1%20OR%201%3D1"

    request sqli GET "/" \
        -H "X-Forwarded-For: 127.0.0.1'\'' OR '\''1'\''='\''1"
}

xss_traffic() {
    section "xss and html injection probes"

    request xss GET "/rest/products/search?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E"
    request xss GET "/rest/products/search?q=%3Csvg%2Fonload%3Dalert(1)%3E"
    request xss GET "/rest/products/search?q=%22%3E%3Cimg%20src%3Dx%20onerror%3Dalert(1)%3E"
    request xss GET "/?next=javascript%3Aalert%281%29"

    request xss POST "/api/Feedbacks/" \
        -H "Content-Type: application/json" \
        --data '{"UserId":1,"captchaId":0,"captcha":"0","comment":"<img src=x onerror=alert(1)>","rating":1}'

    request xss POST "/api/Feedbacks/" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        --data 'comment=%3Cscript%3Ealert(document.domain)%3C%2Fscript%3E&rating=1'

    request xss PUT "/api/Products/1" \
        -H "Content-Type: application/json" \
        --data '{"name":"<svg/onload=alert(1)>","description":"updated by test","price":2.49}'

    request xss PATCH "/api/Users/1" \
        -H "Content-Type: application/json" \
        --data '{"username":"\"><img src=x onerror=alert(1)>"}'
}

lfi_rfi_traffic() {
    section "file inclusion and traversal probes"

    request lfi GET "/ftp/..%2F..%2F..%2F..%2Fetc%2Fpasswd"
    request lfi GET "/?file=../../../../etc/passwd"
    request lfi GET "/?file=..%5C..%5C..%5Cwindows%5Cwin.ini"
    request lfi GET "/?template=php://filter/convert.base64-encode/resource=index.php"
    request lfi GET "/?page=http://127.0.0.1:3000/admin"
    request lfi GET "/?download=/proc/self/environ"

    request lfi POST "/api/Feedbacks/" \
        -H "Content-Type: application/json" \
        --data '{"comment":"../../../../etc/passwd","rating":1}'

    request lfi PUT "/api/Products/1" \
        -H "Content-Type: application/json" \
        --data '{"image":"../../../../etc/passwd","description":"path traversal in body"}'

    request lfi PATCH "/api/Users/1" \
        -H "Content-Type: application/json" \
        --data '{"profileImage":"..\\..\\..\\windows\\win.ini"}'
}

rce_traffic() {
    section "rce and command injection probes"

    request rce GET "/?cmd=cat%20/etc/passwd%3B%20id"
    request rce GET "/?cmd=whoami%26%26uname%20-a"
    request rce GET "/?host=127.0.0.1%7Ccat%20/etc/passwd"
    request rce GET "/rest/products/search?q=%24%28id%29"

    request rce POST "/api/Feedbacks/" \
        -H "Content-Type: application/json" \
        --data '{"comment":"; cat /etc/passwd","rating":1}'

    request rce PUT "/api/Products/1" \
        -H "Content-Type: application/json" \
        --data '{"name":"test","description":"$(id) && cat /etc/passwd","price":1.99}'

    request rce PATCH "/api/Users/1" \
        -H "Content-Type: application/json" \
        --data '{"bio":"`whoami`; uname -a"}'

    request rce DELETE "/api/BasketItems/1?confirm=%60id%60"
}

scanner_and_protocol_traffic() {
    section "scanner, protocol, and header probes"

    request scanner GET "/" \
        -H "User-Agent: sqlmap/1.7 test traffic" \
        -H "X-Forwarded-For: 127.0.0.1'\'' OR '\''1'\''='\''1"

    request scanner GET "/.env" \
        -H "User-Agent: Nikto test traffic"

    request scanner GET "/wp-admin/"
    request scanner GET "/phpmyadmin/"
    request scanner GET "/server-status"
    request scanner TRACE "/"

    request scanner GET "/" \
        -H "X-Original-URL: /admin" \
        -H "X-Rewrite-URL: /admin"

    request scanner POST "/rest/user/login" \
        -H "User-Agent: nmap scripting engine" \
        -H "Content-Type: application/json" \
        --data '{"email":"scan@example.com","password":"scan"}'

    request scanner PUT "/api/Products/1" \
        -H "User-Agent: masscan test traffic" \
        -H "Content-Type: application/json" \
        --data '{"name":"scanner update"}'

    request scanner GET "/" \
        -H "Content-Length: 20" \
        -H "Transfer-Encoding: chunked"
}

deserialization_and_template_traffic() {
    section "deserialization, template, and log4shell-style probes"

    request payload GET "/rest/products/search?q=%24%7Bjndi%3Aldap%3A%2F%2F127.0.0.1%2Fa%7D"
    request payload GET "/rest/products/search?q=%7B%7B7*7%7D%7D"
    request payload GET "/rest/products/search?q=%3C%25%3D%207*7%20%25%3E"

    request payload POST "/api/Feedbacks/" \
        -H "Content-Type: application/json" \
        --data '{"comment":"${jndi:ldap://127.0.0.1/a}","rating":1}'

    request payload POST "/api/Feedbacks/" \
        -H "Content-Type: application/json" \
        --data '{"comment":"O:8:\"stdClass\":0:{}","rating":1}'

    request payload PUT "/api/Products/1" \
        -H "Content-Type: application/json" \
        --data '{"description":"{{constructor.constructor('\''alert(1)'\'')()}}"}'

    request payload PATCH "/api/Users/1" \
        -H "Content-Type: application/json" \
        --data '{"profile":"${jndi:ldap://127.0.0.1/profile}"}'
}

multi_endpoint_method_traffic() {
    section "multi-method endpoint attack coverage"

    request method POST "/api/Users/" \
        -H "Content-Type: application/json" \
        --data '{"email":"attacker+signup@example.com","password":"Password123!","passwordRepeat":"Password123!","securityQuestion":{"id":1,"question":"x","answer":"<script>alert(1)</script>"}}'

    request method POST "/api/Addresss/" \
        -H "Content-Type: application/json" \
        --data '{"UserId":1,"fullName":"../../etc/passwd","mobileNum":"5555555555","zipCode":"12345","streetAddress":"'\'' OR 1=1--","city":"Test","state":"TS","country":"US"}'

    request method POST "/api/Cards/" \
        -H "Content-Type: application/json" \
        --data '{"UserId":1,"fullName":"<img src=x onerror=alert(1)>","cardNum":"4111111111111111","expMonth":12,"expYear":2099}'

    request method POST "/api/BasketItems/" \
        -H "Content-Type: application/json" \
        --data '{"ProductId":"1 OR 1=1","BasketId":1,"quantity":"1; cat /etc/passwd"}'

    request method POST "/api/Quantitys/" \
        -H "Content-Type: application/json" \
        --data '{"ProductId":1,"quantity":"../../../../etc/passwd"}'

    request method POST "/rest/products/reviews" \
        -H "Content-Type: application/json" \
        --data '{"message":"<svg/onload=alert(1)>","author":"reviewer@example.com"}'

    request method PUT "/api/Users/1" \
        -H "Content-Type: application/json" \
        --data '{"email":"put@example.com'\'' OR '\''1'\''='\''1","role":"admin"}'

    request method PUT "/api/Addresss/1" \
        -H "Content-Type: application/json" \
        --data '{"streetAddress":"$(cat /etc/passwd)","city":"Updated","country":"US"}'

    request method PUT "/api/Cards/1" \
        -H "Content-Type: application/json" \
        --data '{"fullName":"\"><script>alert(1)</script>","cardNum":"4111111111111111"}'

    request method PUT "/api/BasketItems/1" \
        -H "Content-Type: application/json" \
        --data '{"quantity":"1 UNION SELECT password FROM Users"}'

    request method PATCH "/api/Users/1" \
        -H "Content-Type: application/json" \
        --data '{"password":"${jndi:ldap://127.0.0.1/password}"}'

    request method PATCH "/api/Products/1" \
        -H "Content-Type: application/json" \
        --data '{"description":"php://filter/convert.base64-encode/resource=index.php"}'

    request method PATCH "/api/BasketItems/1" \
        -H "Content-Type: application/json" \
        --data '{"quantity":"`id`"}'

    request method DELETE "/api/Users/1?force=true%20OR%201%3D1"
    request method DELETE "/api/Products/1?returnTo=javascript%3Aalert(1)"
    request method DELETE "/api/Addresss/1?file=../../../../etc/passwd"
    request method DELETE "/api/Cards/1?cmd=cat%20/etc/passwd"

    request method OPTIONS "/api/Users/" \
        -H "Access-Control-Request-Method: DELETE" \
        -H "Access-Control-Request-Headers: authorization,x-admin"

    request method HEAD "/api/Challenges/"
}

attack_traffic() {
    sqli_traffic
    xss_traffic
    lfi_rfi_traffic
    rce_traffic
    scanner_and_protocol_traffic
    deserialization_and_template_traffic
    multi_endpoint_method_traffic
}

run_pass() {
    normal_traffic
    error_traffic
    attack_traffic
}

run_node_worker() {
    local worker_name="$1"
    local worker_url="$2"

    (
        node_name="$worker_name"
        target_url="$worker_url"

        local start_delay
        start_delay="$(random_stagger)"

        echo "[$node_name] Starting traffic worker in ${start_delay}s against $target_url"

        sleep "$start_delay"

        while true; do
            run_pass
        done
    ) &
}

shutdown() {
    echo
    echo "Stopping traffic generation..."

    trap - INT TERM

    for pid in "${worker_pids[@]:-}"; do
        if kill -0 "$pid" 2>/dev/null; then
            kill -TERM "$pid" 2>/dev/null || true
        fi
    done

    for pid in "${worker_pids[@]:-}"; do
        wait "$pid" 2>/dev/null || true
    done

    if [[ "$manage_compose" == true && "$keep_running" == false ]]; then
        echo "Stopping compose services..."
        podman compose down
    fi

    echo "Stopped."
    exit 0
}

require_command curl
require_command awk

if [[ "$manage_compose" == true ]]; then
    require_command podman

    echo "Starting compose services..."

    podman compose up -d
fi

trap shutdown INT TERM

wait_for_proxy \
    "node-a" \
    "$target_url_a"

wait_for_proxy \
    "node-b" \
    "$target_url_b"

wait_for_proxy \
    "node-c" \
    "$target_url_c"

echo
echo "Generating overlapping traffic across three nodes..."
echo "Press Ctrl+C to stop."
echo

run_node_worker \
    "node-a" \
    "$target_url_a"

worker_pids+=("$!")

run_node_worker \
    "node-b" \
    "$target_url_b"

worker_pids+=("$!")

run_node_worker \
    "node-c" \
    "$target_url_c"

worker_pids+=("$!")

while true; do
    sleep 1
done
