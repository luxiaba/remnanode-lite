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
  arm64-production-runtime \
  native-systemd-install \
  native-openrc-install \
  50000-user-load \
  24h-soak \
  fault-and-rollback-injection; do
  expected_line="- \`${deferred}\`: deferred; not validated by \`docker-production-smoke-v1\`."
  grep -Fxq -- "$expected_line" <<<"$known_risks" || {
    echo "$release_note Known Risks is missing the canonical disclosure: $expected_line" >&2
    exit 1
  }
done

operator_statement='Runtime evidence is operator-attested and is not an unforgeable proof.'
grep -Fxq -- "$operator_statement" <<<"$known_risks" || {
  echo "$release_note Known Risks is missing the canonical operator evidence statement" >&2
  exit 1
}
