#!/usr/bin/env bash
# A0 check for the gateway (PLANO.md §12). Run from this folder while the gateway is running:
#   (set -a; . ./.env; set +a; bash a0-check.sh)        # checks + lists events
#   (set -a; . ./.env; set +a; bash a0-check.sh poll)   # also sends you a test poll first
#   (set -a; . ./.env; set +a; bash a0-check.sh poll 1203…@g.us)   # same, inside a group
#   (set -a; . ./.env; set +a; bash a0-check.sh groups) # creates a test community + topic group with you as admin
#   (set -a; . ./.env; set +a; bash a0-check.sh groups 1203…@g.us)  # reuses that community, creates only the topic group
# A 429 (rate-overlimit) means WhatsApp is limiting this number: wait hours, do not retry in a loop.
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

# field NAME -> prints a top-level string field of the last response
field() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get(sys.argv[2], ""))' "$body" "$1" 2>/dev/null; }

# show_group -> prints the last response (a group) without its invite link or other secrets
show_group() {
	python3 - "$body" <<'PY'
import json, sys
g = json.load(open(sys.argv[1]))
if not g.get("jid"):
    sys.exit(0)  # the FAIL line above already shows the error
print(f"      {g.get('kind')} {g.get('name')!r} jid={g.get('jid')} parent={g.get('parent_jid', '-')}")
for p in g.get("participants", []):
    print(f"         member pn={p.get('pn', '-')} lid={p.get('lid', '-')} admin={p.get('is_admin')} error={p.get('error', 0)}")
PY
}

if [ "${1:-}" = "poll" ]; then
	echo "== 3b. Poll"
	target="${2:-$OWNER_PHONE}"
	check "bridge token sends a poll to $target" 200 \
		"$(call POST /v1/polls "$TOKEN_BRIDGE" "{\"to\":\"$target\",\"question\":\"A0: qual modelo?\",\"options\":[\"Haiku\",\"Sonnet\",\"Opus\"]}")"
	echo "      -> on your phone: vote, change the vote, then uncheck it; then run this script again without 'poll'"
fi

if [ "${1:-}" = "groups" ]; then
	echo "== 3c. Community and topic group"
	community="${2:-}"
	if [ -n "$community" ]; then
		check "community $community exists" 200 "$(call GET "/v1/groups/$community" "$TOKEN_BRIDGE")"
		[ "$(field kind)" = "community" ] || community=""
	else
		check "create a community" 200 "$(call POST /v1/groups "$TOKEN_BRIDGE" '{"name":"🧪 A0 Comunidade","description":"Teste A0 do gateway","community":true}')"
		community=$(field jid)
	fi
	show_group
	if [ -n "$community" ]; then
		check "create a group in it with you" 200 \
			"$(call POST /v1/groups "$TOKEN_BRIDGE" "{\"name\":\"🧪 A0 Tópico\",\"participants\":[\"$OWNER_PHONE\"],\"parent\":\"$community\"}")"
		topic=$(field jid)
		show_group
	fi
	if [ -n "${topic:-}" ]; then
		check "promote you to admin" 200 \
			"$(call POST "/v1/groups/$topic/participants" "$TOKEN_BRIDGE" "{\"action\":\"promote\",\"participants\":[\"$OWNER_PHONE\"]}")"
		# Joining the topic group made you a community member; as its admin you can delete the test community later.
		check "promote you in the community" 200 \
			"$(call POST "/v1/groups/$community/participants" "$TOKEN_BRIDGE" "{\"action\":\"promote\",\"participants\":[\"$OWNER_PHONE\"]}")"
		check "read the group back" 200 "$(call GET "/v1/groups/$topic" "$TOKEN_BRIDGE")"
		show_group
		check "get an invite link (not shown)" 200 "$(call GET "/v1/groups/$topic/invite" "$TOKEN_BRIDGE")"
		echo "      -> on your phone: send a message in '🧪 A0 Tópico' and reply quoting it;"
		echo "         then: bash a0-check.sh poll $topic   (vote in the group), and run without arguments to list events."
		echo "         When done, delete both test groups from your phone (you are admin)."
	fi
fi

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
    if e["type"] == "poll_vote":
        v = d.get("voter", {})
        print(f"      #{e['id']} poll_vote  poll={d.get('poll_message_id')}  voter pn={v.get('pn', '-')} lid={v.get('lid', '-')}")
        print(f"         selected {d.get('selected')}  chat={d.get('chat_jid')}")
        continue
    if e["type"] == "group_joined":
        by = d.get("added_by") or {}
        print(f"      #{e['id']} group_joined  {d.get('kind')} {d.get('name')!r} jid={d.get('jid')} parent={d.get('parent_jid', '-')} added_by pn={by.get('pn', '-')}")
        continue
    s = d.get("sender", {})
    print(f"      #{e['id']} {e['type']}  chat={d.get('chat_kind')}  parent={d.get('group_parent_jid', '-')}  from_me={d.get('is_from_me')}  edit_of={d.get('edit_of', '-')}")
    if d.get("chat_kind") != "dm":
        print(f"         chat_jid {d.get('chat_jid')}")
    print(f"         sender  pn={s.get('pn', '-')}  lid={s.get('lid', '-')}  name={s.get('push_name', '-')}")
    print(f"         text    {d.get('text', '')[:80]!r}")
    if "quoted" in d:
        q = d["quoted"]
        print(f"         quoted  id={q.get('message_id')}  sender={q.get('sender', '-')}  text={q.get('text', '')[:60]!r}")
    if "attachment" in d:
        print(f"         attach  {d['attachment']}")

mine = [e["data"] for e in events if e["type"] == "message" and e["data"].get("chat_kind") == "dm" and not e["data"].get("is_from_me")]
votes = [e["data"] for e in events if e["type"] == "poll_vote"]
print("      -- what A0 needs to know:")
print(f"      your DMs to the bot: {len(mine)}")
print(f"      arrived with your phone (pn = OWNER_PHONE): {any(m.get('sender', {}).get('pn') == owner for m in mine)}")
print(f"      arrived with a LID: {any('lid' in m.get('sender', {}) for m in mine)}")
print(f"      a quote was read: {any('quoted' in m for m in mine)}")
print(f"      poll votes decrypted: {len(votes)}")
print(f"      an unchecked vote arrived as empty: {any(v.get('selected') == [] for v in votes)}")
in_groups = [e["data"] for e in events if e["type"] == "message" and e["data"].get("chat_kind") == "group" and not e["data"].get("is_from_me")]
print(f"      group_joined events: {sum(e['type'] == 'group_joined' for e in events)}")
print(f"      your messages in a group: {len(in_groups)}")
print(f"        with your phone: {any(m.get('sender', {}).get('pn') == owner for m in in_groups)}")
print(f"        with a LID: {any('lid' in m.get('sender', {}) for m in in_groups)}")
print(f"        with the community as parent: {any('group_parent_jid' in m for m in in_groups)}")
print(f"        a quote was read: {any('quoted' in m for m in in_groups)}")
print(f"      poll votes in a group: {sum(v.get('chat_jid', '').endswith('@g.us') for v in votes)}")
PY

echo "== Result: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
