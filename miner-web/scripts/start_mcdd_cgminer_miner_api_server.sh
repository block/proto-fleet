#!/usr/bin/env bash
set -ex

# Build cgminer:
cd /usr/src/cgminer && ./autogen.sh && CFLAGS="-O2 -Wall -fcommon" ./configure --enable-generic-miner && make -j 8
# Build mcdd:
cd /usr/src/mcdd && cargo build --release
# Run cgminer: (pointed to localhost running local_testchain OR stratum_test_server by default)
/usr/src/cgminer/cgminer -c /usr/src/cgminer/cgminer.conf --text-only &
# Run MCDD in background:
cargo run --release &
# Run miner-api-server in watch mode
cd /usr/src/miner-api-server && watchexec -r -w src -e rs 'cargo run -- --ip-addr 0.0.0.0'
