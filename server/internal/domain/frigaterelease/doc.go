// Package frigaterelease verifies signed Frigate release statements and reads
// Debian package control data.
//
// A release statement is a small canonical JSON document, schema
// "frigate-release-statement/v1", that binds one Debian package (name, version,
// architecture, SHA-256 and size) to a repository and a minimum installed
// version. Release infrastructure signs the exact statement bytes with an ECDSA
// P-256 key: the signature is ASN.1 DER over the SHA-256 of the raw statement.
// A key ID is "sha256:" plus the lowercase hex SHA-256 of the public key's DER
// SubjectPublicKeyInfo.
//
// Fleet's check is advisory. It rejects a bad package at upload time, before
// anything is sent to a controller; every controller verifies the signature
// again against its own trusted keys. The package uses only the Go standard
// library.
package frigaterelease
