#!/usr/bin/env bash
set -ex

# Build cgminer:
cd /usr/src/cgminer && ./autogen.sh && CFLAGS="-O2 -Wall -fcommon" ./configure --enable-generic-miner && make -j 8
# Build mcdd:
cd /usr/src/mcdd && cargo build --release
# Run cgminer: (pointed to localhost running local_testchain OR stratum_test_server by default)
# miner-api-server needs to restart cgminer so it needs to exist as a service
cat <<EOF > /etc/systemd/system/cgminer.service
[Unit]
Description=CGMiner Startup Service
After=network.target

[Service]
ExecStart=/usr/src/cgminer/cgminer -c /usr/src/cgminer/cgminer.conf --text-only
Restart=always
User=root

[Install]
WantedBy=multi-user.target
EOF

# Start cgminer service:
systemctl enable cgminer && systemctl start cgminer

# Run MCDD in background:
cargo run --release &
# Run miner-api-server in watch mode
cd /usr/src/miner-api-server && watchexec -r -w src -e rs 'cargo run -- --ip-addr 0.0.0.0 --cgminer-config-path /usr/src/cgminer/cgminer.conf'
