# Cloud-staged application releases

Operators verify a published bundle locally, upload it to private object storage,
and pin its repository, tag and checksum in deployment configuration. CI verifies
and stages the bundle for normal upgrades. GitHub credentials stay on the operator's
machine; CI and hosts need none.

Configure each host's updater with a systemd drop-in at
`/etc/systemd/system/proto-fleet-updater.service.d/release-dir.conf`:

```ini
[Service]
Environment="PROTO_FLEET_UPDATER_RELEASE_DIR=/var/lib/proto-fleet-releases"
```

Create the directory using the ownership rules below, then run
`sudo systemctl daemon-reload` and `sudo systemctl restart proto-fleet-updater`.
Keep this drop-in in host provisioning so replacement hosts receive it too.
`fleet-ha install` preserves this separate file but rewrites
`/etc/proto-fleet/updater.env`; do not put the release directory setting there.
For a manually launched updater, use `--release-dir`.

Stage bundles under the configured directory:

```text
<release-dir>/<version>/proto-fleet-<version>-<arch>.tar.gz
<release-dir>/<version>/proto-fleet-<version>-<arch>.tar.gz.sha256
```

Use the host architecture, `amd64` or `arm64`, for `<arch>`.

Use a separate absolute, symlink-free directory per repository, owned by root or
the updater account. Neither the directory nor its files may be group/world writable.
The updater copies files into protected storage before checksum, archive and
source-pin verification. Missing or invalid files fail without network fallback;
unset the directory to restore public GitHub downloads.

Version rules, repository pins, locks, fencing and HA recovery are unchanged.
Repository switches, nightlies and downgrades require a separate operator maintenance
procedure; neither CI upgrades nor the ordinary updater perform those transitions.

Manifest-covered `cloud-release.json` records `schema_version` from
`server/migrations/current/` and a `compatibility_sha256` over current/retained
migrations, bridge SQL, baseline assertions/reconciliation inputs and listed
DB/Patroni/etcd sources. The format is unchanged. Automated database retention
requires an exact match; retained history affects the hash, not the version.

Bundles include `server/fleet-db-transition` for [offline adoption](../../docs/development/baseline-upgrade.md).
`update-preflight` validates the candidate release, profile and Compose model
before reconciliation; `fleet-ha app-start`, including recovery, checks the target
schema before starting containers. After reconciliation, recover by completing
the qualified target deployment or restoring the coordinated backup.

A match does not account for floating image or installed package versions and
does not certify compatibility. Operators must still manually qualify migrations
and database dependencies for rare cross-deploys, even when fingerprints match.
