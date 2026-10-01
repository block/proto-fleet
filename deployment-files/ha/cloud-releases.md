# Cloud-staged application releases

Operators verify a published bundle locally, upload it to private object storage,
and pin its repository, tag and checksum in deployment configuration. CI verifies
and stages the bundle for normal upgrades. GitHub credentials stay on the operator's
machine; CI and hosts need none.

Set `PROTO_FLEET_UPDATER_RELEASE_DIR` (or `--release-dir`) on the host updater to use
staged bundles instead of public GitHub downloads:

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

Manifest-covered `cloud-release.json` records the schema version and one fingerprint
covering all up/down migrations and database/Patroni/etcd build/configuration sources.
Cloud deployment requires exact matches to retain the database during replacement.
Matching metadata neither proves application compatibility nor permits database
upgrades or downgrades.
