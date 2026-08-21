#!/usr/bin/env node
// Generate locally-trusted TLS certs for the Vite dev server's opt-in HTTPS mode
// (`VITE_HTTPS=true`). Writes client/certs/{localhost,localhost-key}.pem.
//
// Run once (or whenever your hostname changes) with: npm run setup:https
//
// The cert covers loopback (localhost / 127.0.0.1 / ::1) plus this machine's
// hostname(s), so it also works when the server is exposed with `vite --host`
// and reached by hostname. LAN IPs are intentionally omitted: they rotate with
// DHCP, which would silently invalidate the cert. Reach the machine by hostname
// instead, or re-run this script if you must pin a new address.
import { spawnSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import os from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const clientDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const certsDir = resolve(clientDir, "certs");

// mkcert must be installed and its local CA trusted (`mkcert -install`).
const version = spawnSync("mkcert", ["-version"], { encoding: "utf8" });
if (version.error) {
  console.error(
    "mkcert is not installed or not on PATH.\n" +
      "  macOS:  brew install mkcert nss   (nss adds Firefox trust)\n" +
      "  then:   mkcert -install           (one-time, trusts the local CA)\n" +
      "See https://github.com/FiloSottile/mkcert for other platforms.",
  );
  process.exit(1);
}

// Loopback always; hostname(s) so `--host` access by name works. No LAN IPs.
const hosts = new Set(["localhost", "127.0.0.1", "::1"]);
if (os.hostname()) hosts.add(os.hostname());

// macOS advertises an mDNS/Bonjour name that often differs from os.hostname().
if (process.platform === "darwin") {
  const localName = spawnSync("scutil", ["--get", "LocalHostName"], { encoding: "utf8" });
  if (localName.status === 0 && localName.stdout.trim()) {
    hosts.add(`${localName.stdout.trim()}.local`);
  }
}

mkdirSync(certsDir, { recursive: true });

const hostList = [...hosts];
console.log(`Generating dev certs for: ${hostList.join(", ")}`);
const result = spawnSync(
  "mkcert",
  ["-key-file", "certs/localhost-key.pem", "-cert-file", "certs/localhost.pem", ...hostList],
  { cwd: clientDir, stdio: "inherit" },
);

if (result.status !== 0) process.exit(result.status ?? 1);
console.log(
  "\nDone. Start the dev server over HTTPS with:\n  VITE_HTTPS=true npm run dev:protoFleet   # or: VITE_HTTPS=true npm run dev",
);
