#!/bin/bash
cd /workspaces/mvp/testdata/aria2_competitive_benchmark
A="--no-conf --file-allocation=none --allow-overwrite=true --auto-file-renaming=false --enable-color=false --summary-interval=1 --console-log-level=notice --max-tries=3 --retry-wait=1 --timeout=30 --connect-timeout=30 --http-accept-gzip=false --check-certificate=false --disable-ipv6=true --conditional-get=false"
for split in 1 2 4; do
  ./tools/bench_run -system aria2 -aria2-timeout 1500s -aria2-args "$A --min-split-size=1M --max-connection-per-server=$split --split=$split" -run-id af-split$split -out discovery_aria2/af-split$split.json
done
./tools/bench_run -system aria2 -aria2-timeout 1500s -aria2-args "$A --min-split-size=32M --max-connection-per-server=4 --split=4" -run-id af-split4-min32M -out discovery_aria2/af-split4-min32M.json
echo "=== RERUN DONE"
