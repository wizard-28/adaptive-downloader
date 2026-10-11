#!/bin/bash
cd /workspaces/mvp/testdata/n_dependent_server_joint_ns
B=./tools/ns_bench
$B -system ours -policy none -n 4 -chunk 4194304 -schedule t1 -cap-c 8388608 -cap-gamma 0.05 -cap-at 0 -run-id smoke_capped_4m -out logs/smoke/capped_4m.json
$B -system ours -policy classifier -n 16 -chunk 1048576 -healthy-n 16 -healthy-s 1048576 -capped-n 4 -capped-s 4194304 -threshold 460800 -schedule t1 -cap-c 0 -run-id smoke_sel_healthy -out logs/smoke/sel_healthy.json
$B -system ours -policy classifier -n 16 -chunk 1048576 -healthy-n 16 -healthy-s 1048576 -capped-n 4 -capped-s 4194304 -threshold 460800 -schedule t1 -cap-c 8388608 -cap-gamma 0.05 -cap-at 0 -run-id smoke_sel_capped -out logs/smoke/sel_capped.json
$B -system ours -policy drop -n 16 -chunk 1048576 -capped-n 4 -capped-s 1048576 -drop 0.5 -schedule t1 -cap-c 8388608 -cap-gamma 0.05 -cap-at 8 -run-id smoke_drop -out logs/smoke/drop.json
echo SMOKE2_DONE
