#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 RELEASE_NOTE" >&2
  exit 2
fi

release_note="$1"
[ -s "$release_note" ] || {
  echo "missing or empty release note: $release_note" >&2
  exit 1
}

known_risks="$(awk '
  /^## Known Risks$/ { in_known_risks = 1; next }
  in_known_risks && /^## / { exit }
  in_known_risks { print }
' "$release_note")"
[ -n "$known_risks" ] || {
  echo "$release_note has an empty Known Risks section" >&2
  exit 1
}

for deferred in \
  whole-host-512mib-runtime \
  arm64-production-runtime \
  native-systemd-install \
  native-openrc-install \
  50000-user-load \
  24h-soak \
  fault-and-rollback-injection; do
  expected_line="- \`${deferred}\`: deferred; not validated by \`docker-production-smoke-v2\`."
  grep -Fxq -- "$expected_line" <<<"$known_risks" || {
    echo "$release_note Known Risks is missing the canonical disclosure: $expected_line" >&2
    exit 1
  }
done

host_scope_statement='The smoke validates the canonical container limits on the recorded host; whole-host 512 MiB / 1 vCPU / 2 GB runtime remains deferred.'
grep -Fxq -- "$host_scope_statement" <<<"$known_risks" || {
  echo "$release_note Known Risks is missing the canonical host-scope statement" >&2
  exit 1
}

operator_statement='Runtime evidence is operator-attested and is not an unforgeable proof.'
grep -Fxq -- "$operator_statement" <<<"$known_risks" || {
  echo "$release_note Known Risks is missing the canonical operator evidence statement" >&2
  exit 1
}
