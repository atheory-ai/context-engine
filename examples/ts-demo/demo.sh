#!/usr/bin/env bash
# A guided walk through IIR verification: declared intent vs. real TypeScript.
# One clean function passes; three drifted copies each get caught.
#
# Requires a `ce` binary with the default plugins embedded. Point CE at yours,
# or put `ce` on PATH:
#     CE=/path/to/ce ./demo.sh
set -euo pipefail

CE="${CE:-ce}"
HERE="$(cd "$(dirname "$0")" && pwd)"
INTENT="$HERE/intents/validateDonation.iir.yaml"

if ! command -v "$CE" >/dev/null 2>&1 && [ ! -x "$CE" ]; then
	echo "error: '$CE' not found. Build it (see README) and set CE=/path/to/ce." >&2
	exit 1
fi

hr() { printf '\n──────────────────────────────────────────────────────────\n'; }

# verify <label> <source> — run the verifier, showing only the report (the
# stderr plugin-load notes are suppressed for a clean transcript).
verify() {
	local label="$1" src="$2"
	hr
	echo "▶ $label"
	echo "  ce iir verify intents/validateDonation.iir.yaml $(basename "$src")"
	echo
	if "$CE" iir verify "$INTENT" "$src" 2>/dev/null; then
		echo "  → exit 0 (verification passed)"
	else
		echo "  → non-zero exit (verification failed)"
	fi
}

echo "Context Engine — IIR verify demo"
echo "The intent in intents/validateDonation.iir.yaml declares validateDonation's"
echo "contract: its inputs, return type, structured behavior, and that it is pure."

verify "CLEAN — the source matches the intent" "$HERE/src/validateDonation.ts"
verify "DRIFT 1 — an undeclared side effect was added (logger.info)" "$HERE/src/drift-undeclared-effect.ts"
verify "DRIFT 2 — the branch condition was flipped (< became >)" "$HERE/src/drift-changed-behavior.ts"
verify "DRIFT 3 — the public contract changed (no longer exported; return type changed)" "$HERE/src/drift-changed-contract.ts"

hr
echo "The intent stayed fixed; only the source drifted — and each divergence was"
echo "surfaced (side effect and contract breaks as errors, the behavior change as"
echo "a warning). That is the IIR verification loop."
