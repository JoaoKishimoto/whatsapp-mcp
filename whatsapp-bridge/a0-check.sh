#!/usr/bin/env bash
# A0 check for the gateway (PLANO.md §12). Run from this folder while the gateway is running:
#   (set -a; . ./.env; set +a; bash a0-check.sh)
# Reads TOKEN_BRIDGE, TOKEN_NOTIFY and OWNER_PHONE from the environment and never prints the tokens.
# Read-only on the queue: it does not ack events, so you can run it again.
set -u

BASE="${GATEWAY_URL:-http://localhost:8080}"
: "${TOKEN_BRIDGE:?TOKEN_BRIDGE is not set}"
: "${TOKEN_NOTIFY:?TOKEN_NOTIFY is not set}"
: "${OWNER_PHONE:?OWNER_PHONE is not set}"

pass=0
fail=0
body=$(mktemp)
trap 'rm -f "$body"' EXIT

# call METHOD PATH TOKEN [JSON] -> prints the HTTP status (000 if unreachable); response body goes to $body
call() {
	local args=(-sS -o "$body" -w '%{http_code}' -X "$1" "$BASE$2")
	[ -n "$3" ] && args+=(-H "Authorization: Bearer $3")
	[ $# -ge 4 ] && args+=(-H 'Content-Type: application/json' --data "$4")
	curl "${args[@]}" 2>/dev/null
}

# check NAME EXPECTED_STATUS GOT_STATUS
check() {
	if [ "$3" = "$2" ]; then
		echo "PASS  $1"
		pass=$((pass + 1))
	else
		echo "FAIL  $1 (expected HTTP $2, got $3) $(head -c 300 "$body")"
		fail=$((fail + 1))
	fi
}

echo "== 1. Connection"
code=$(call GET /v1/health "")
check "gateway answers on $BASE" 200 "$code"
if grep -Eq '"connected": *true' "$body" && grep -Eq '"logged_in": *true' "$body"; then
	echo "PASS  connected and logged in"
	pass=$((pass + 1))
else
	echo "FAIL  not connected/logged in: $(cat "$body")"
	fail=$((fail + 1))
fi

echo "== 2. Tokens"
check "no token is refused" 401 "$(call GET /v1/events "")"
check "notify token cannot read events" 403 "$(call GET /v1/events "$TOKEN_NOTIFY")"

echo "== 3. Sending"
# "to" points elsewhere on purpose: with the notify token it must be ignored and go to OWNER_PHONE.
check "notify token sends a message" 200 \
	"$(call POST /v1/messages "$TOKEN_NOTIFY" '{"to":"5500000000000","text":"A0: teste do gateway (token notify). Se chegou aqui, o envio funciona."}')"
echo "      -> confirm on your phone that the message arrived in your chat with the bot"

echo "== 4. Receiving (messages sent to the bot since the gateway started)"
check "bridge token reads events" 200 "$(call GET '/v1/events?after=0' "$TOKEN_BRIDGE")"
python3 - "$body" "$OWNER_PHONE" <<'PY'
import json, sys

try:
    events = json.load(open(sys.argv[1]))
except ValueError:
    sys.exit(0)  # already reported as FAIL above
owner = sys.argv[2]
if not events:
    print("      (no events yet: from your phone, message the bot, then reply quoting that message, and run again)")
    sys.exit(0)

for e in events:
    d = e["data"]
    s = d.get("sender", {})
    print(f"      #{e['id']} {e['type']}  chat={d.get('chat_kind')}  from_me={d.get('is_from_me')}  edit_of={d.get('edit_of', '-')}")
    print(f"         sender  pn={s.get('pn', '-')}  lid={s.get('lid', '-')}  name={s.get('push_name', '-')}")
    print(f"         text    {d.get('text', '')[:80]!r}")
    if "quoted" in d:
        q = d["quoted"]
        print(f"         quoted  id={q.get('message_id')}  sender={q.get('sender', '-')}  text={q.get('text', '')[:60]!r}")
    if "attachment" in d:
        print(f"         attach  {d['attachment']}")

mine = [e["data"] for e in events if e["data"].get("chat_kind") == "dm" and not e["data"].get("is_from_me")]
print("      -- what A0 needs to know:")
print(f"      your DMs to the bot: {len(mine)}")
print(f"      arrived with your phone (pn = OWNER_PHONE): {any(m.get('sender', {}).get('pn') == owner for m in mine)}")
print(f"      arrived with a LID: {any('lid' in m.get('sender', {}) for m in mine)}")
print(f"      a quote was read: {any('quoted' in m for m in mine)}")
PY

echo "== Result: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
