# Cloud-staged application releases

An external deployment coordinator can set
`PROTO_FLEET_UPDATER_RELEASE_DIR` (or `--release-dir`) on the host updater. It stages:

```text
<release-dir>/<version>/proto-fleet-<version>-amd64.tar.gz
<release-dir>/<version>/proto-fleet-<version>-amd64.tar.gz.sha256
```

Use an absolute, symlink-free path, owned by root or the updater account and not
group/world writable. Keep separate roots for separate publishing repositories.
The updater copies the files into its protected working directory and verifies
them through the existing checksum, archive and source-pin checks. Missing local
files fail; there is no network fallback. With no directory configured, public
GitHub downloads work as before.

This transport does not permit repository switches, nightly installation or
downgrades through the ordinary updater. Its existing semantic-version, locking,
repository-pinning, fencing and HA recovery rules remain unchanged.

Published bundles also include manifest-covered `cloud-release.json`: a format
version, the latest schema version, a fingerprint of every up/down migration, and
a fingerprint of the database/Patroni/etcd build and configuration sources. Cloud
orchestration can conservatively require exact matches before retaining an existing
database during application replacement. These fingerprints do not prove arbitrary
application compatibility and do not authorize database upgrades or downgrades.
