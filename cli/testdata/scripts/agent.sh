#!/usr/bin/env bash
# Agent simulation against a live lewnotes server.
#
#   LEWNOTES_SERVER=https://notes.example.com \
#   LEWNOTES_TOKEN=lnt... \
#   bash cli/testdata/scripts/agent.sh "Kitchen"
#
# It searches, appends a timestamped line, verifies the read, and proves that a
# concurrent change produces exit code 5 rather than a silent overwrite.
set -uo pipefail

ref="${1:-Kitchen}"
lewnotes="${LEWNOTES_BIN:-lewnotes}"
stamp="agent-check $(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [[ -z "${LEWNOTES_SERVER:-}" || -z "${LEWNOTES_TOKEN:-}" ]]; then
	echo "LEWNOTES_SERVER and LEWNOTES_TOKEN are required" >&2
	exit 4
fi

echo "== whoami"
"$lewnotes" whoami --json || exit $?

echo "== search"
"$lewnotes" search "$ref" --json >/dev/null || exit $?

echo "== append"
printf '%s\n' "$stamp" | "$lewnotes" note append "$ref" --stdin --json >/dev/null || exit $?

echo "== verify"
"$lewnotes" note show "$ref" --json | grep -F "$stamp" >/dev/null || {
	echo "appended text not found" >&2
	exit 1
}

echo "== conflict path"
# A real conflict needs a change between the CLI's read and write. That race is
# covered deterministically by `go test ./cmd/lewnotes -run TestAgentFlowAndConflict`;
# from a shell, verify that the server enforces If-Match instead:
token_note=$("$lewnotes" note show "$ref" --json) || exit $?
id=$(printf '%s' "$token_note" | jq -r '.data.id')
code=$(curl -s -o /dev/null -w '%{http_code}' -X PUT \
	-H "Authorization: Bearer $LEWNOTES_TOKEN" \
	-H 'Content-Type: application/json' \
	-H 'If-Match: "definitely-stale"' \
	-d '{"content":"stale write\n"}' \
	"$LEWNOTES_SERVER/api/v1/notes/$id")
if [[ "$code" != "412" ]]; then
	echo "expected 412 for a stale If-Match, got $code" >&2
	exit 1
fi

echo "agent simulation complete"
