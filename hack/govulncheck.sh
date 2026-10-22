#!/usr/bin/env bash
# Copyright 2026 BitWise Media Group Ltd
# SPDX-License-Identifier: MIT
#
# govulncheck with a reviewed acceptance list. govulncheck has no ignore
# mechanism of its own, so this wrapper fails on any advisory whose
# vulnerable symbols are actually reachable from our code EXCEPT the ones
# accepted below — each of which needs a reason and a trigger for deleting
# it again.
#
# Accepted:
#   GO-2026-6809  crypto/x509 (stdlib): distrust compromised intermediate
#                 certificates. Published 2026-10-21, fixed in go1.27.3; the
#                 go pin (mise.toml, go.mod, the toolchain's mise.lock) is
#                 still 1.27.2 and go1.27.3 is inside the 7-day release
#                 cooldown. Reachable only through TLS verification of dex's
#                 serving certificate, which chains to the operator-supplied
#                 CA (--dex-ca-file), not the public web PKI the advisory
#                 concerns. Delete this entry when the go pin moves to
#                 1.27.3 or later.
set -euo pipefail

accepted="GO-2026-6809"

out="$(mktemp "${TMPDIR:-/tmp}/govulncheck.XXXXXX")"
trap 'rm -f "$out"' EXIT

govulncheck -format json ./... > "$out"

# Symbol-level findings only (trace[0].function set): package- and
# module-level matches are what `govulncheck ./...` reports as informative,
# not failures.
called="$(jq -r 'select(.finding and .finding.trace[0].function) | .finding.osv' "$out" | sort -u)"

fail=0
for osv in $called; do
  case " $accepted " in
    *" $osv "*) ;;
    *)
      echo "govulncheck: $osv reaches called symbols (see https://pkg.go.dev/vuln/$osv)" >&2
      fail=1
      ;;
  esac
done

if [ "$fail" -ne 0 ]; then
  echo "govulncheck: run 'govulncheck ./...' for traces; fix or accept in hack/govulncheck.sh with a reason" >&2
  exit 1
fi

for osv in $accepted; do
  if echo "$called" | grep -qxF "$osv"; then
    echo "govulncheck: $osv reachable — accepted (see hack/govulncheck.sh)" >&2
  fi
done
echo "govulncheck: clean (accepted: $accepted)" >&2
