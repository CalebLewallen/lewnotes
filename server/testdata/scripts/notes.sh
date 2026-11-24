#!/usr/bin/env bash
# Live smoke for the notes API. Never prints passwords, cookies, or tokens.
#
#   LEWNOTES_URL=http://127.0.0.1:8080 \
#   LEWNOTES_PASSWORD='...' server/testdata/scripts/notes.sh
#
# If the instance has no owner yet, the script sets one up with that password.
set -euo pipefail

BASE="${LEWNOTES_URL:-http://127.0.0.1:8080}"
PASSWORD="${LEWNOTES_PASSWORD:-correct horse battery staple}"
TMP="$(mktemp -d)"
JAR="$TMP/cookies"
trap 'rm -rf "$TMP"' EXIT

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

json_get() {
	python3 -c 'import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {}, {"d": d}))' "$1"
}

csrf_token() {
	awk -F'\t' '$6 == "lewnotes_csrf" { print $7 }' "$JAR" | tail -n 1
}

setup_required="$(curl -fsS "$BASE/api/v1/auth/status" | json_get 'd["setup_required"]')"
if [ "$setup_required" = "True" ]; then
	curl -fsS -c "$JAR" -o /dev/null -H 'Content-Type: application/json' \
		-d "{\"password\":\"$PASSWORD\",\"instance_name\":\"smoke\"}" \
		"$BASE/api/v1/auth/setup"
	echo "setup: owner created"
else
	curl -fsS -c "$JAR" -o /dev/null -H 'Content-Type: application/json' \
		-d "{\"password\":\"$PASSWORD\"}" "$BASE/api/v1/auth/login"
	echo "auth: signed in"
fi
CSRF="$(csrf_token)"
[ -n "$CSRF" ] || fail "no CSRF cookie after auth"
RUN="$(date +%s)-$$"
WELCOME_PATH="inbox/Welcome-$RUN.md"
SMOKE_PATH="inbox/Smoke-$RUN.md"

api() {
	curl -sS -b "$JAR" -c "$JAR" "$@"
}

create_note() {
	local path="$1" content="$2"
	local payload
	payload="$(python3 -c 'import json,sys; print(json.dumps({"path":sys.argv[1],"content":sys.argv[2]}))' "$path" "$content")"
	api -D "$TMP/headers" -H 'Content-Type: application/json' -H "X-Lewnotes-CSRF: $CSRF" \
		-d "$payload" "$BASE/api/v1/notes"
}

# Two notes so the smoke has a resolved wikilink and a backlink.
welcome="$(create_note "$WELCOME_PATH" '# Welcome

This vault is served by lewnotes-core.')"
welcome_id="$(printf '%s' "$welcome" | json_get 'd["id"]')"
welcome_etag="$(awk 'tolower($1) == "etag:" { print $2 }' "$TMP/headers" | tr -d '\r' | tail -n 1)"
smoke="$(create_note "$SMOKE_PATH" "# Smoke

Links to [[Welcome-$RUN]] and [[Missing-$RUN]].")"
smoke_id="$(printf '%s' "$smoke" | json_get 'd["id"]')"
[ -n "$welcome_id" ] && [ -n "$smoke_id" ] || fail "note creation did not return ids"

etag="$(awk 'tolower($1) == "etag:" { print $2 }' "$TMP/headers" | tr -d '\r' | tail -n 1)"
[ -n "$etag" ] && [ -n "$welcome_etag" ] || fail "create response had no ETag"

# The link to Welcome must resolve (exact for a root note, suffix for a
# nested one); Missing must stay a ghost.
api "$BASE/api/v1/notes/$smoke_id" | grep -Eq '"resolution":"(exact|suffix)"' ||
	fail "wikilink did not resolve"
api "$BASE/api/v1/notes/$smoke_id" | grep -q '"resolution":"unresolved"' ||
	fail "ghost link was not indexed"

code="$(api -o /dev/null -w '%{http_code}' -X PUT -H 'Content-Type: application/json' \
	-H "X-Lewnotes-CSRF: $CSRF" -d '{"content":"# Smoke v2"}' "$BASE/api/v1/notes/$smoke_id")"
[ "$code" = "428" ] || fail "PUT without If-Match returned $code, want 428"

code="$(api -o /dev/null -w '%{http_code}' -X PUT -H 'Content-Type: application/json' \
	-H "X-Lewnotes-CSRF: $CSRF" -H 'If-Match: "stale"' \
	-d '{"content":"# Smoke v2"}' "$BASE/api/v1/notes/$smoke_id")"
[ "$code" = "412" ] || fail "stale PUT returned $code, want 412"

api -D "$TMP/headers" -o /dev/null -X PUT -H 'Content-Type: application/json' \
	-H "X-Lewnotes-CSRF: $CSRF" -H "If-Match: $etag" \
	-d "{\"content\":\"# Smoke v2\\n\\nStill [[Welcome-$RUN]].\"}" \
	"$BASE/api/v1/notes/$smoke_id"
etag="$(awk 'tolower($1) == "etag:" { print $2 }' "$TMP/headers" | tr -d '\r' | tail -n 1)"

api -D "$TMP/headers" -o /dev/null -X PATCH -H 'Content-Type: application/json' \
	-H "X-Lewnotes-CSRF: $CSRF" -H "If-Match: $etag" \
	-d '{"ops":[{"type":"append","text":"patched line"}]}' "$BASE/api/v1/notes/$smoke_id"
etag="$(awk 'tolower($1) == "etag:" { print $2 }' "$TMP/headers" | tr -d '\r' | tail -n 1)"
api "$BASE/api/v1/notes/$smoke_id" | grep -q 'patched line' ||
	fail "PATCH append did not persist"

api "$BASE/api/v1/tree?depth=5" | grep -q '"path":"inbox"' || fail "tree is missing inbox"
api "$BASE/api/v1/search?q=$RUN" | grep -q "\"path\":\"$SMOKE_PATH\"" ||
	fail "search did not find the smoke note"
api "$BASE/api/v1/notes/$welcome_id/backlinks" | grep -q "\"path\":\"$SMOKE_PATH\"" ||
	fail "backlink from Smoke to Welcome is missing"
api "$BASE/api/v1/notes/$welcome_id/links" | grep -q '"links"' || fail "links endpoint failed"

code="$(api -o /dev/null -w '%{http_code}' -X DELETE -H "X-Lewnotes-CSRF: $CSRF" \
	-H "If-Match: $etag" "$BASE/api/v1/notes/$smoke_id")"
[ "$code" = "200" ] || fail "delete returned $code, want 200"
api -o /dev/null -X DELETE -H "X-Lewnotes-CSRF: $CSRF" -H "If-Match: $welcome_etag" \
	"$BASE/api/v1/notes/$welcome_id"

echo "notes smoke: passed"
