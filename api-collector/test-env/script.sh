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

section() {
    printf "\n== %s ==\n" "$1"
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
