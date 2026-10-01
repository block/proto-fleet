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

Rare cross-deploys require operators to manually qualify the source and target
releases, including migration and database dependency compatibility, before
retaining a database. Release bundles do not provide an automated compatibility
fingerprint for that decision.
