# HA writer-generation proof fixture

This disposable, local-only fixture proves the writer-generation contract needed
before Proto Fleet adds its database lease. It is the HA-07 slice of the active/
passive implementation plan, not a production deployment template.

## Contract

A pair of linearizable etcd prefix reads of `/service/proto-fleet-ha07/`
brackets the runtime identity checks:

1. The first `leader` key value names the Patroni member that owns the DCS lock.
2. The `leader` key's etcd `create_revision` is the writer generation. It is
   stable while that leader key exists and strictly increases when an expired or
   deleted lock is recreated for a later leadership term.
3. The matching `members/<leader>` value supplies Patroni's PostgreSQL and REST
   endpoints at that first DCS snapshot revision.
4. A connection through the multi-host, `target_session_attrs=read-write` DSN
   must select the address advertised by that member.
5. Patroni's `/primary` check must pass, and its timeline must equal
   `pg_control_checkpoint().timeline_id` on the connected writable PostgreSQL.
6. A second linearizable prefix read must still report the same leader name and
   `create_revision`; otherwise leadership changed during validation and the
   observation fails closed.

The observation also reads the current leader lease TTL. The proof observes the
same lease's TTL decrease and then increase, demonstrating a keepalive refresh
without requiring Patroni to rewrite the leader key. The observation fails
closed if any part is absent, expired, stale, or inconsistent.
`leader.mod_revision` is retained only for diagnostics; it is not the generation
because Patroni may update the leader key while preserving the same leadership
term.

The full serialized Fleet ownership token will additionally contain the cluster
incarnation and lease epoch. Restoring etcd can roll revisions backward, so a
restore must rotate the cluster incarnation as specified by the later HA restore
work.

## What the proof runs

`scripts/prove.sh` creates a unique Compose project containing:

- two PostgreSQL 18 + TimescaleDB + Patroni 4.1.4 members;
- a three-member etcd 3.6.11 quorum; and
- no Fleet process, keepalived, installer, production network policy, or
  production image lifecycle.

The script proves:

- the generation is stable across Patroni lease renewal;
- repeated primary promotions strictly increase it;
- an acknowledged write can be lost during asynchronous promotion while the
  generation still increases;
- isolating the old primary from the DCS/replication network promotes the peer
  with a higher generation; and
- reconnecting the old primary makes it rejoin as a replica without changing
  the new writer term.

The fixture intentionally uses test-only credentials and an isolated Docker
network. Do not reuse it as an installer or production configuration.

## Run it

Docker with Compose is required. From the repository root:

```bash
deployment-files/ha/generation-fixture/scripts/prove.sh unit
deployment-files/ha/generation-fixture/scripts/prove.sh config >/dev/null
deployment-files/ha/generation-fixture/scripts/prove.sh run
```

The full run rebuilds the repository's TimescaleDB image, creates a uniquely
named project, prints each observed generation transition, emits the final
validated observation as JSON, and removes its containers and volumes. A
teardown failure makes the command fail instead of silently leaking resources.

Set `HA_GENERATION_KEEP_STACK=1` to retain a failed fixture for inspection.
Use `HA_GENERATION_CLEAN_PROMOTIONS=<n>` to increase the repeated-promotion
count; the default is two before the separate write-loss and partition trials.
`HA_GENERATION_WAIT_SECONDS` bounds each wait, while
`HA_GENERATION_PROBE_TIMEOUT_SECONDS` bounds individual Docker, PostgreSQL, and
Patroni probes.

## Upstream basis

- [Patroni 4.1.4](https://github.com/patroni/patroni/tree/v4.1.4) creates the
  etcd leader key only when its create revision is zero and attaches the
  current Patroni lease to that key.
- [etcd's v3 key-value API](https://etcd.io/docs/v3.6/learning/api/) defines
  `create_revision` as the key's creation revision and makes range reads
  linearizable unless `serializable` is explicitly requested.
- [Patroni's REST API](https://patroni.readthedocs.io/en/latest/rest_api.html)
  returns success from `/primary` only for the PostgreSQL primary that owns the
  leader lock.
