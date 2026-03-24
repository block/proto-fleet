/**
 * Fleet Onboarding, Discovery, and Pairing Script
 *
 * End-to-end script that bootstraps a Fleet instance by:
 * 1. Authenticating (creating an admin user via REST if needed)
 * 2. Resolving a discovery target subnet (from env or the NetworkInfo REST endpoint)
 * 3. Running nmap-based device discovery via Connect-RPC streaming
 * 4. Pairing newly discovered devices (all or Proto-only, based on env config)
 * 5. Reporting the final fleet inventory
 *
 * Auth and onboarding use the REST API; discovery and pairing use Connect-RPC.
 *
 * Environment variables:
 *   FLEET_API_URL            – server base URL (default: http://localhost:4000)
 *   FLEET_ADMIN_USERNAME     – admin username (default: admin)
 *   FLEET_ADMIN_PASSWORD     – admin password (default: Pass123!)
 *   FLEET_SESSION_COOKIE     – skip auth and use an existing session cookie
 *   FLEET_DISCOVERY_TARGET   – subnet/IP to scan (default: auto-detected via NetworkInfo)
 *   FLEET_DISCOVERY_PORTS    – comma-separated ports to scan (default: server-advertised ports)
 *   FLEET_PAIR_ALL_DISCOVERED – "true" to pair all devices, not just Proto rigs
 *
 * Run with:
 *   npx tsx client/scripts/auth_discover_pair.ts
 */

import { create } from "@bufbuild/protobuf";
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { DeviceIdentifierListSchema } from "../src/protoFleet/api/generated/common/v1/device_selector_pb";
import {
  FleetManagementService,
  ListMinerStateSnapshotsRequestSchema,
  PairingStatus,
} from "../src/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceSelectorSchema } from "../src/protoFleet/api/generated/minercommand/v1/command_pb";
import {
  PairRequestSchema,
  PairingService,
  DiscoverRequestSchema,
  type Device,
} from "../src/protoFleet/api/generated/pairing/v1/pairing_pb";

const baseUrl = process.env.FLEET_API_URL ?? "http://localhost:4000";
const adminUsername = process.env.FLEET_ADMIN_USERNAME ?? "admin";
const adminPassword = process.env.FLEET_ADMIN_PASSWORD ?? "Pass123!";
const requestedSessionCookie = process.env.FLEET_SESSION_COOKIE;
const requestedDiscoveryTarget = process.env.FLEET_DISCOVERY_TARGET;
const requestedDiscoveryPorts = process.env.FLEET_DISCOVERY_PORTS;
const discoveryPorts = requestedDiscoveryPorts
  ? requestedDiscoveryPorts
      .split(",")
      .map((port) => port.trim())
      .filter(Boolean)
  : [];
const pairAllDiscovered = process.env.FLEET_PAIR_ALL_DISCOVERED === "true";

const transport = createConnectTransport({ baseUrl });
const pairingClient = createClient(PairingService, transport);
const fleetClient = createClient(FleetManagementService, transport);

type FleetInitStatusResponse = {
  status?: {
    adminCreated?: boolean;
  };
};

type NetworkInfoResponse = {
  networkInfo?: {
    subnet?: string;
    localIp?: string;
    gateway?: string;
  };
};

type AuthenticateResponse = {
  userInfo?: {
    username?: string;
  };
  sessionExpiry?: string | number;
};

async function postJson<T>(
  path: string,
  body: unknown,
  sessionCookie?: string,
): Promise<{ data: T; setCookie: string | null }> {
  const headers = new Headers({
    "Content-Type": "application/json",
  });

  if (sessionCookie) {
    headers.set("Cookie", sessionCookie);
  }

  const response = await fetch(`${baseUrl}${path}`, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  });

  const responseText = await response.text();
  if (!response.ok) {
    throw new Error(`${path} failed with ${response.status}: ${responseText}`);
  }

  return {
    data: responseText ? (JSON.parse(responseText) as T) : ({} as T),
    setCookie: response.headers.get("set-cookie"),
  };
}

function formatSessionCookie(rawCookie: string): string {
  if (rawCookie.includes("=")) {
    return rawCookie;
  }

  return `fleet_session=${rawCookie}`;
}

function normalizeSubnetCIDR(value: string): string {
  const [ip, prefixString] = value.split("/");
  if (!ip || !prefixString) {
    return value;
  }

  const octets = ip.split(".").map((part) => Number(part));
  const prefix = Number(prefixString);
  const validIPv4 =
    octets.length === 4 && octets.every((octet) => Number.isInteger(octet) && octet >= 0 && octet <= 255);
  if (!validIPv4 || !Number.isInteger(prefix) || prefix < 0 || prefix > 32) {
    return value;
  }

  const ipInt = octets.reduce((acc, octet) => (acc << 8) | octet, 0) >>> 0;
  const mask = prefix === 0 ? 0 : (0xffffffff << (32 - prefix)) >>> 0;
  const network = ipInt & mask;

  const normalizedOctets = [(network >>> 24) & 0xff, (network >>> 16) & 0xff, (network >>> 8) & 0xff, network & 0xff];

  return `${normalizedOctets.join(".")}/${prefix}`;
}

// Step 1: Authenticate — reuse an existing cookie, or onboard + login via REST
async function ensureSessionCookie(): Promise<string> {
  if (requestedSessionCookie) {
    const cookie = formatSessionCookie(requestedSessionCookie);
    console.log(`Using existing Fleet session cookie for ${baseUrl}`);
    return cookie;
  }

  const initStatus = await postJson<FleetInitStatusResponse>("/onboarding.v1.OnboardingService/GetFleetInitStatus", {});

  if (!initStatus.data.status?.adminCreated) {
    console.log(`Creating Fleet admin user ${adminUsername}...`);
    await postJson("/onboarding.v1.OnboardingService/CreateAdminLogin", {
      username: adminUsername,
      password: adminPassword,
    });
  } else {
    console.log(`Fleet already onboarded. Authenticating as ${adminUsername}...`);
  }

  const authResponse = await postJson<AuthenticateResponse>("/auth.v1.AuthService/Authenticate", {
    username: adminUsername,
    password: adminPassword,
  });

  if (!authResponse.setCookie) {
    throw new Error("Authenticate succeeded but no session cookie was returned.");
  }

  const sessionCookie = authResponse.setCookie.split(";")[0];
  const username = authResponse.data.userInfo?.username ?? adminUsername;
  console.log(`Authenticated as ${username}. Session cookie captured.`);
  return sessionCookie;
}

// Step 2: Resolve subnet — use the env override or query the NetworkInfo REST endpoint
async function getDiscoveryTarget(sessionCookie: string): Promise<string> {
  if (requestedDiscoveryTarget) {
    return requestedDiscoveryTarget;
  }

  const response = await postJson<NetworkInfoResponse>(
    "/networkinfo.v1.NetworkInfoService/GetNetworkInfo",
    {},
    sessionCookie,
  );

  const subnet = response.data.networkInfo?.subnet;
  if (!subnet) {
    throw new Error("Network info response did not include a subnet.");
  }

  const normalizedSubnet = normalizeSubnetCIDR(subnet);
  console.log(
    `Using discovery target ${normalizedSubnet} (backend reported subnet ${subnet}, local IP ${response.data.networkInfo?.localIp ?? "unknown"}).`,
  );
  return normalizedSubnet;
}

// Step 3: Discover devices via nmap Connect-RPC streaming
async function discoverDevices(sessionCookie: string, target: string): Promise<Device[]> {
  const request = create(DiscoverRequestSchema, {
    mode: {
      case: "nmap",
      value:
        discoveryPorts.length > 0
          ? {
              target,
              ports: discoveryPorts,
            }
          : {
              target,
            },
    },
  });

  const discovered = new Map<string, Device>();

  for await (const response of pairingClient.discover(request, {
    headers: {
      Cookie: sessionCookie,
    },
  })) {
    if (response.error) {
      console.warn(`Discovery warning: ${response.error}`);
    }

    for (const device of response.devices) {
      discovered.set(device.deviceIdentifier, device);
    }
  }

  return [...discovered.values()];
}

// Step 4: Pair selected devices via Connect-RPC
async function pairDevices(sessionCookie: string, devices: Device[]): Promise<string[]> {
  if (devices.length === 0) {
    return [];
  }

  const request = create(PairRequestSchema, {
    deviceSelector: create(DeviceSelectorSchema, {
      selectionType: {
        case: "includeDevices",
        value: create(DeviceIdentifierListSchema, {
          deviceIdentifiers: devices.map((device) => device.deviceIdentifier),
        }),
      },
    }),
  });

  const response = await pairingClient.pair(request, {
    headers: {
      Cookie: sessionCookie,
    },
  });

  return response.failedDeviceIds;
}

// Step 5: List current fleet inventory via Connect-RPC
async function listMiners(sessionCookie: string) {
  const response = await fleetClient.listMinerStateSnapshots(
    create(ListMinerStateSnapshotsRequestSchema, {
      pageSize: 100,
    }),
    {
      headers: {
        Cookie: sessionCookie,
      },
    },
  );

  return response.miners;
}

function summarizeDevices(label: string, devices: Device[]) {
  const counts = new Map<string, number>();

  for (const device of devices) {
    const key = `${device.driverName}:${device.manufacturer} ${device.model}`;
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }

  console.log(`${label}: ${devices.length}`);
  for (const [key, count] of [...counts.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
    console.log(`  ${count} x ${key}`);
  }
}

async function main() {
  console.log(`Bootstrapping Fleet fake-miner setup against ${baseUrl}`);

  const sessionCookie = await ensureSessionCookie();
  const discoveryTarget = await getDiscoveryTarget(sessionCookie);

  console.log(`Scanning ${discoveryTarget} on ports ${discoveryPorts.join(", ")}...`);
  const discoveredDevices = await discoverDevices(sessionCookie, discoveryTarget);
  summarizeDevices("Discovered devices", discoveredDevices);

  const currentMiners = await listMiners(sessionCookie);
  const alreadyPairedIds = new Set(
    currentMiners
      .filter((miner) => miner.pairingStatus === PairingStatus.PAIRED)
      .map((miner) => miner.deviceIdentifier),
  );

  const candidateDevices = pairAllDiscovered
    ? discoveredDevices
    : discoveredDevices.filter((device) => device.driverName === "proto");
  const devicesToPair = candidateDevices.filter((device) => !alreadyPairedIds.has(device.deviceIdentifier));

  summarizeDevices(pairAllDiscovered ? "Pairing all discovered devices" : "Pairing Proto devices", devicesToPair);

  if (devicesToPair.length === 0) {
    console.log("No newly discovered devices need pairing.");
    console.log(
      `Fleet now has ${currentMiners.length} miner snapshot(s), including ${currentMiners.filter((miner) => miner.driverName === "proto").length} Proto rig(s).`,
    );
    return;
  }

  const failedDeviceIds = await pairDevices(sessionCookie, devicesToPair);
  if (failedDeviceIds.length > 0) {
    console.warn(`Pairing failed for ${failedDeviceIds.length} device(s): ${failedDeviceIds.join(", ")}`);
  } else {
    console.log("Pairing completed without failures.");
  }

  const miners = await listMiners(sessionCookie);
  const pairedProtoMiners = miners.filter((miner) => miner.driverName === "proto");
  console.log(`Fleet now has ${miners.length} miner snapshot(s), including ${pairedProtoMiners.length} Proto rig(s).`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
