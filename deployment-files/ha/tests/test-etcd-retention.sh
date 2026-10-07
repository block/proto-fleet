#!/usr/bin/env bash
set -euo pipefail

# Opt-in local integration smoke: requires Docker and Python 3. Uses the pinned
# etcd image, a disposable container, and a loopback-only random port; no volumes.
# Accelerate the production 1h policy to 5s. This tests etcd's contract, not a
# full Patroni failover or a production-size workload.
HA_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 - "$HA_DIR/compose.yaml" <<'PY'
import base64
import json
import re
import subprocess
import sys
import time
import urllib.request

with open(sys.argv[1]) as compose:
    image = re.search(r"image: (gcr.io/etcd-development/etcd:\S+)", compose.read()).group(1)
container = subprocess.check_output([
    "docker", "run", "--rm", "-d", "-p", "127.0.0.1::2379", image,
    "/usr/local/bin/etcd", "--name=retention-smoke", "--data-dir=/tmp/etcd",
    "--listen-client-urls=http://0.0.0.0:2379",
    "--advertise-client-urls=http://127.0.0.1:2379",
    "--auto-compaction-mode=periodic", "--auto-compaction-retention=5s",
], text=True).strip()
try:
    url = "http://" + subprocess.check_output(
        ["docker", "port", container, "2379"], text=True).strip()
    prefix = "/service/proto-fleet/"
    token = None

    def encode(value):
        return base64.b64encode(value.encode()).decode()

    def request(path, payload):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = token
        return urllib.request.urlopen(urllib.request.Request(
            url + "/v3/" + path, data=json.dumps(payload).encode(), headers=headers), timeout=10)

    def rpc(path, payload=None):
        with request(path, payload or {}) as response:
            return json.load(response)

    def put(value):
        return rpc("kv/put", {"key": encode(prefix + "members/ha-a"), "value": encode(value)})

    def sizes(status):
        return {key: int(status[key]) for key in ("dbSize", "dbSizeInUse", "dbSizeQuota")}

    for attempt in range(50):
        try:
            rpc("maintenance/status")
            break
        except Exception:
            if attempt == 49:
                raise
            time.sleep(0.1)

    lease = rpc("lease/grant", {"TTL": 120})["ID"]
    rpc("kv/put", {"key": encode(prefix + "leader"), "value": encode("ha-a"), "lease": lease})
    for revision in range(250):
        put(str(revision) + "x" * 16384)
    print("before compaction:", sizes(rpc("maintenance/status")), flush=True)

    # Wait for automatic logical compaction, rather than interpreting delayed
    # backend size statistics as evidence that compaction has or has not run.
    deadline = time.monotonic() + 30
    while True:
        with request("watch", {"create_request": {
            "key": encode(prefix), "range_end": encode("/service/proto-fleet0"),
            "start_revision": "2",
        }}) as response:
            message = json.loads(response.readline())["result"]
            if message.get("created"):
                message = json.loads(response.readline())["result"]
        if message.get("canceled") and int(message.get("compact_revision", 0)) >= 2:
            break
        assert time.monotonic() < deadline, "automatic compaction did not cancel the old watch"
        time.sleep(0.5)

    snapshot = rpc("kv/range", {"key": encode(prefix), "range_end": encode("/service/proto-fleet0")})
    assert len(snapshot["kvs"]) == 2, "compaction removed a current key"
    current = {entry["key"]: entry["value"] for entry in snapshot["kvs"]}
    assert current[encode(prefix + "leader")] == encode("ha-a")
    assert current[encode(prefix + "members/ha-a")] == encode("249" + "x" * 16384)
    assert int(rpc("lease/timetolive", {"ID": lease})["TTL"]) > 0
    with request("watch", {"create_request": {
        "key": encode(prefix), "range_end": encode("/service/proto-fleet0"),
        "start_revision": str(int(snapshot["header"]["revision"]) + 1),
    }}) as response:
        assert json.loads(response.readline())["result"]["created"]
        put("recovered")
        event = json.loads(response.readline())["result"]["events"][0]
        assert event["kv"]["value"] == encode("recovered")

    allocated = int(rpc("maintenance/status")["dbSize"])
    for revision in range(100):
        put(str(revision) + "y" * 16384)
    reused = sizes(rpc("maintenance/status"))
    assert reused["dbSize"] <= allocated, "overwrites did not reuse compacted space"
    print("after reusing compacted space:", reused, flush=True)
    rpc("maintenance/defragment")
    defragmented = sizes(rpc("maintenance/status"))
    assert defragmented["dbSize"] < reused["dbSize"], "manual defrag did not reclaim allocated space"
    print("after manual defrag:", defragmented, flush=True)

    # Match the deployed observer's read-only prefix permission. Maintenance
    # Status must work without granting the observer an administrator role.
    def ctl(*args):
        subprocess.run(["docker", "exec", container, "etcdctl", *args],
                       check=True, stdout=subprocess.DEVNULL)

    ctl("user", "add", "root:test-root-password")
    ctl("user", "grant-role", "root", "root")
    ctl("role", "add", "fleet-observer")
    ctl("role", "grant-permission", "fleet-observer", "read", prefix, "--prefix")
    ctl("user", "add", "fleet-observer:test-observer-password")
    ctl("user", "grant-role", "fleet-observer", "fleet-observer")
    ctl("auth", "enable")
    token = rpc("auth/authenticate", {"name": "fleet-observer", "password": "test-observer-password"})["token"]
    status = rpc("maintenance/status")
    assert int(status["dbSizeQuota"]) > 0
    assert not status.get("errors"), status
    assert len(rpc("kv/range", {"key": encode(prefix), "range_end": encode("/service/proto-fleet0")})["kvs"]) == 2
    print("PASS: current keys/lease, stale-watch relist, fresh watch, space reuse, manual defrag, observer status")
finally:
    subprocess.run(["docker", "rm", "-f", container], check=True, stdout=subprocess.DEVNULL)
PY
