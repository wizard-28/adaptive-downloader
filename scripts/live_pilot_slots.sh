#!/usr/bin/env bash
# Runs the L-P1 live-pilot slots at their protocol start times (IST).
# Usage: scripts/live_pilot_slots.sh BIN "S2=2026-10-18T21:00" "S1T=2026-10-18T13:00@tuna" ...
# An optional "@id,id" suffix restricts the slot to those servers.
# Each slot is resumable: re-running skips runs already in raw/<slot>.jsonl.
set -u
bin=$1; shift
cd "$(dirname "$0")/.."
log=testdata/live_pilot_l1/logs
mkdir -p "$log"
for spec in "$@"; do
  slot=${spec%%=*}; at=${spec#*=}
  servers=""
  case $at in *@*) servers=${at#*@}; at=${at%%@*} ;; esac
  target=$(TZ=Asia/Kolkata date -d "$at" +%s)
  now=$(date +%s)
  if [ "$target" -gt "$now" ]; then
    echo "$(date -u +%FT%TZ) waiting for $slot at $at IST"
    sleep $((target - now))
  fi
  echo "$(date -u +%FT%TZ) start $slot servers=${servers:-all}"
  if [ -n "$servers" ]; then
    "$bin" -slot "$slot" -servers "$servers" >>"$log/$slot.log" 2>&1
  else
    "$bin" -slot "$slot" >>"$log/$slot.log" 2>&1
  fi
  echo "$(date -u +%FT%TZ) end $slot exit=$? runs=$(wc -l < testdata/live_pilot_l1/raw/$slot.jsonl 2>/dev/null || echo 0)"
done
