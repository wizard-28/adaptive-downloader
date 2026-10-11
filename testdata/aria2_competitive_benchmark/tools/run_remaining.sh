#!/bin/bash
cd /workspaces/mvp/testdata/aria2_competitive_benchmark
for b in aria2frontier oursfrontier final mechanism; do
  echo "=== BLOCK $b START $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  python3 tools/run_benchmark.py $b
  echo "=== BLOCK $b EXIT:$? $(date -u +%Y-%m-%dT%H:%M:%SZ)"
done
echo "=== ALL REMAINING BLOCKS DONE $(date -u +%Y-%m-%dT%H:%M:%SZ)"
