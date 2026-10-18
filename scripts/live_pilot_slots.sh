#!/usr/bin/env bash
# Runs the L-P1 live-pilot slots at their protocol start times (IST).
# Usage: scripts/live_pilot_slots.sh BIN "S2=2026-10-18T21:00" "S3=2026-10-19T08:00" ...
# Each slot is resumable: re-running skips runs already in raw/<slot>.jsonl.
set -u
bin=$1; shift
cd "$(dirname "$0")/.."
log=testdata/live_pilot_l1/logs
mkdir -p "$log"
for spec in "$@"; do
  slot=${spec%%=*}; at=${spec#*=}
  target=$(TZ=Asia/Kolkata date -d "$at" +%s)
  now=$(date +%s)
  if [ "$target" -gt "$now" ]; then
    echo "$(date -u +%FT%TZ) waiting for $slot at $at IST"
    sleep $((target - now))
  fi
  echo "$(date -u +%FT%TZ) start $slot"
  "$bin" -slot "$slot" >>"$log/$slot.log" 2>&1
  echo "$(date -u +%FT%TZ) end $slot exit=$? runs=$(wc -l < testdata/live_pilot_l1/raw/$slot.jsonl 2>/dev/null || echo 0)"
done
