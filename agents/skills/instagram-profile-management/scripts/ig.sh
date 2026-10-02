#!/usr/bin/env bash
# Thin wrapper over the Instagram API with Instagram Login (graph.instagram.com).
# The token is read from $IG_ACCESS_TOKEN and sent as a header, never on the command line.
#
#   bash ig.sh GET  "/me?fields=user_id,username,account_type"
#   bash ig.sh POST "/<IG_ID>/media" '{"image_url":"https://...","caption":"hi"}'
#   bash ig.sh DELETE "/<IG_COMMENT_ID>"
#   bash ig.sh wait-container <IG_CONTAINER_ID>
#   bash ig.sh refresh-token
set -euo pipefail

if [ -z "${IG_ACCESS_TOKEN:-}" ]; then
  echo "IG_ACCESS_TOKEN not set. Ask the user to run: ! export IG_ACCESS_TOKEN=<token>" >&2
  exit 2
fi

base="https://graph.instagram.com/${IG_API_VERSION:-v25.0}"

pretty() {
  if command -v jq >/dev/null 2>&1; then jq .; else cat; fi
}

call() {
  local method="$1" path="$2" body="${3:-}"
  local args=(-sS -X "$method" -H "Authorization: Bearer ${IG_ACCESS_TOKEN}")
  if [ -n "$body" ]; then
    args+=(-H "Content-Type: application/json" -d "$body")
  fi
  curl "${args[@]}" "${base}${path}"
}

case "${1:-}" in
  wait-container)
    id="${2:?usage: ig.sh wait-container <IG_CONTAINER_ID>}"
    # Meta recommends polling at most once a minute, for no more than 5 minutes.
    for _ in 1 2 3 4 5 6; do
      out="$(call GET "/${id}?fields=status_code,status")"
      code="$(printf '%s' "$out" | sed -n 's/.*"status_code":"\([A-Z_]*\)".*/\1/p')"
      echo "status_code=${code:-unknown}" >&2
      case "$code" in
        FINISHED|PUBLISHED) printf '%s\n' "$out" | pretty; exit 0 ;;
        ERROR|EXPIRED|"") printf '%s\n' "$out" | pretty; exit 1 ;;
      esac
      sleep 60
    done
    echo "container ${id} still IN_PROGRESS after 5 minutes" >&2
    exit 1
    ;;
  refresh-token)
    # Unversioned endpoint; token must be long-lived, >=24h old, not expired.
    # Prints the new token's JSON. The caller decides whether to re-export it.
    curl -sS -G "https://graph.instagram.com/refresh_access_token" \
      --data-urlencode "grant_type=ig_refresh_token" \
      --data-urlencode "access_token=${IG_ACCESS_TOKEN}" | pretty
    ;;
  GET|POST|DELETE)
    [ $# -ge 2 ] || { echo "usage: ig.sh <GET|POST|DELETE> <path> [json-body]" >&2; exit 2; }
    call "$1" "$2" "${3:-}" | pretty
    ;;
  *)
    echo "usage: ig.sh <GET|POST|DELETE> <path> [json-body] | wait-container <id> | refresh-token" >&2
    exit 2
    ;;
esac
