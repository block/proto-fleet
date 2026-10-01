# Cloud-staged application releases

Set `PROTO_FLEET_UPDATER_RELEASE_DIR` (or `--release-dir`) on the host updater to use
staged bundles instead of public GitHub downloads:

```text
<release-dir>/<version>/proto-fleet-<version>-amd64.tar.gz
<release-dir>/<version>/proto-fleet-<version>-amd64.tar.gz.sha256
```

Use an absolute, symlink-free directory owned by root or the updater account;
neither it nor its files may be group/world writable. Use a separate root per
publishing repository. Files are copied into protected updater storage before
checksum, archive and source-pin verification. Invalid or missing files fail
without network fallback. Unset the directory to use public GitHub downloads.

This changes delivery only: version rules, repository pins, locks, fencing and HA
recovery still apply. The ordinary updater cannot switch repositories, install
nightlies or downgrade.

Manifest-covered `cloud-release.json` records the format/schema versions and hashes
of all up/down migrations and database/Patroni/etcd build/configuration sources.
Cloud orchestration uses exact matches to retain the database during application
replacement. Matching metadata does not prove application compatibility or permit
database upgrades/downgrades.
