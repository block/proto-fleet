---
title: Replace Nmap with shared TCP discovery
date: 2026-09-09
tracker: https://github.com/block/proto-fleet/pull/1033
status: implementing
type: plan
---

# Replace Nmap with shared TCP discovery

Fleet needs TCP port reachability before its existing miner plugins identify devices. Implement that narrow operation in the existing Go server module, shared by Fleet Nodes and Fleet Server. No third-party scanning dependency or additional scanning features are needed.

All five PRs remain drafts, stacked on the manual remote-discovery work in PR #1030. Deploy the complete stack and update all components together. Preserve existing command protocol versions, capability checks, upgrade indicators, and other maintenance infrastructure; add no migration-specific aliases or compatibility shims.

## PR 1: Shared scanning and targets

Add `internal/domain/netscan` with lazy IPv4 CIDR/range iteration, eligible IPv6 literals, target grammar, DNS policy, normalized ports, and report-scope matching. Replace `nmaptarget`. Keep the current /22 and 1,024-target limits. One reusable scanner per process limits all requests to 512 simultaneous TCP connections, each with a three-second timeout. Close connections immediately, serialize host callbacks, drain workers before returning terminal errors, and distinguish normal closed ports from local resource or permission failures.

CIDRs through /30 omit only their actual network and broadcast addresses; /31 and /32 include every address. Explicit literals/ranges include .0 and .1. Resolve hostnames once, filtering unusable answers and node-ineligible public addresses before preferring IPv4. Enforce the ten-port raw input cap before deduplication.

## PR 2: Fleet Node discovery and packaging

Use the shared TCP scanner for network mode. IP-list and IP-range modes send validated targets directly to plugins so virtual miners without TCP listeners remain discoverable. Both paths use bounded existing plugin probing (32 concurrent probes, ten seconds each), with shared target parsing, DNS policy, and port validation. Keep the ten-minute command budget and supervisor. Retain identified reports on scan failure or deadline, upload once at completion in batches of 1,024 under one 30-second final-upload budget, then ACK. Upload failure wins over scan failure; deadline/supervisor truncation reports PARTIAL. Remove node binary lookup, Nmap execution, installer requirements, CI installation, and node image packages. Preserve explicit subnet scanning on Windows and existing subnet detection behavior.

## PR 3: Fleet Server discovery and packaging

Use the shared TCP scanner for network mode and direct plugin probing for explicit IP lists and ranges. Both paths reuse target resolution and bounded per-host plugin identification with serialized persistence, preserving duplicate suppression and collision fallback. Bound shutdown even when a plugin ignores cancellation. Keep known-subnet aggregate discovery and existing server address policy without adding an aggregate target cap. Interleave server and Fleet Node network targets and choose a random starting address within each target on every request, wrapping around without retaining scan progress. Retries can cover different addresses but may repeat work. Keep already displayed miners when rescanning, and suggest retrying or narrowing the range on timeout. Remove the Go Nmap dependency and remaining runtime packages. Drop NET_RAW for scanning containers; retain unrelated fake-miner and HA networking capabilities.

## PR 4: Incomplete discovery warnings

Rename response field 2 from `error` to `warning`, regenerating all outputs. Runtime source failures and PARTIAL node ACKs produce nonterminal warnings while retaining devices and continuing other sources. Validation, authentication, and stream failures remain RPC errors; user cancellation stays quiet. Pass PARTIAL through the existing command callback without a new outcome framework, and preserve pairing behavior. Ensure warnings survive deduplication and show through the existing UI notification pattern. Configure both discovery RPCs with 13-minute read timeouts and no buffering in all three nginx configurations.

## PR 5: Final contract and qualification

Raise the shared prefix minimum to /20 and derive the 4,096-target cap from it. Keep ten ports and 1,024 reports per upload; permit 40,960 reports per command. Rename `NmapModeRequest`/`nmap` to `NetworkScanModeRequest`/`network_scan` at existing field 4 and regenerate all outputs. Update active documentation, scripts, and policy references. Do not rewrite unrelated historical records.

Verify /20 versus /19, 4,096 versus 4,097 targets, ten versus eleven ports, and that forty upload batches plus the ACK fit the existing 64-event queue. Do not promise every /20 completes within ten minutes: responsive ports with slow plugins can consume the budget.

## Validation

Run targeted unit and race tests for scanner, target semantics, node scanning/reporting, server streaming, command ACK propagation, report scope, forwarder warnings, and client notifications. Compile the Windows node. Run canonical generation, formatting, and relevant lint/type checks. Leave database integration and hardware/network qualification to CI or an explicitly available test environment; report unrun qualification honestly. Review the complete diff before publishing the draft stack. No production deployment or merge is part of this work.
