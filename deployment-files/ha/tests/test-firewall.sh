#!/usr/bin/env bash
set -euo pipefail

HA_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Keep the test's rules and sockets out of the host network namespace.
exec unshare --net python3 - "${HA_DIR}/firewall.nft.tmpl" <<'PY'
import socket
import subprocess
import sys
import unittest
from pathlib import Path

subprocess.run(["ip", "link", "set", "lo", "up"], check=True)
rules = Path(sys.argv.pop()).read_text()
for name, value in {
    "HA_DB_A_IP": "127.0.0.1",
    "HA_DB_B_IP": "127.0.0.2",
    "HA_DCS_C_IP": "127.0.0.3",
    "HA_NODE_IP": "127.0.0.1",
    "HA_NETWORK_INTERFACE": "lo",
}.items():
    rules = rules.replace("${" + name + "}", value)
subprocess.run(["nft", "-f", "-"], input=rules, text=True, check=True)


class FirewallTest(unittest.TestCase):
    def test_patroni_replies_reach_ephemeral_debug_port(self):
        with socket.socket() as server:
            server.bind(("127.0.0.1", 8008))
            server.listen()
            server.settimeout(2)
            for port in (39999, 40000):
                with self.subTest(source_port=port), socket.socket() as client:
                    client.settimeout(2)
                    client.bind(("127.0.0.2", port))
                    client.connect(server.getsockname())
                    connection, _ = server.accept()
                    with connection:
                        connection.sendall(b"x")
                        self.assertEqual(client.recv(1), b"x")

    def test_new_debug_connections_remain_blocked(self):
        with socket.socket() as server, socket.socket() as client:
            server.bind(("127.0.0.1", 40000))
            server.listen()
            client.settimeout(0.5)
            client.bind(("127.0.0.2", 0))
            with self.assertRaises(TimeoutError):
                client.connect(server.getsockname())


unittest.main()
PY
