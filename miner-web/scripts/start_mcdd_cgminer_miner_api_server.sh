#!/usr/bin/env bash
set -ex

# Build cgminer:
cd /usr/src/cgminer && ./autogen.sh && CFLAGS="-O2 -Wall -fcommon" ./configure --enable-generic-miner && make -j 8
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

# Create MCDD empty database
cd /usr/src/crates/mcdd ; cargo run -- -c

# Run MCDD in watch mode
# mcdd/build.rs makes changes to the src/usb/pb/ directory which triggers a rebuild of the entire project so ignore that folder
# cd /usr/src/crates/mcdd && watchexec -r -w src -e rs --ignore 'src/usb/pb/*' 'cargo run' &
cd /usr/src/crates/mcdd && cargo run &

# Run miner-api-server in watch mode
cd /usr/src/crates/miner-api-server && watchexec -r -w src -e rs --project-origin . 'cargo run -- --ip-addr 0.0.0.0 --cgminer-config-path /usr/src/cgminer/cgminer.conf'
