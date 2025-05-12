/**
 * Discovery and Pairing Test Script
 *
 * This script demonstrates how to use the Connect-RPC client to:
 * 1. Check if an admin user exists, create one if not
 * 2. Authenticate with the server
 * 3. Discover miners on the network
 * 4. Pair with discovered miners
 *
 * Run this script with:
 * npx tsx src/discover-pair.ts
 */

import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { create } from "@bufbuild/protobuf";

async function main() {
  // Importing the required services
  console.log("Loading services...");

  // Dynamic imports to avoid issues with ESM/CJS compatibility
  const authModule = await import(
    "../src/protoFleet/api/generated/auth/v1/auth_pb"
  );
  const pairingModule = await import(
    "../src/protoFleet/api/generated/pairing/v1/pairing_pb"
  );
  const onboardingModule = await import(
    "../src/protoFleet/api/generated/onboarding/v1/onboarding_pb"
  );

  const OnboardingService = onboardingModule.OnboardingService;
  const CreateAdminLoginRequestSchema =
    onboardingModule.CreateAdminLoginRequestSchema;
  const AuthService = authModule.AuthService;
  const PairingService = pairingModule.PairingService;
  const AuthenticateRequestSchema = authModule.AuthenticateRequestSchema;
  const DiscoverRequestSchema = pairingModule.DiscoverRequestSchema;
  const IPListModeRequestSchema = pairingModule.IPListModeRequestSchema;
  const MDNSModeRequestSchema = pairingModule.MDNSModeRequestSchema;
  const NmapModeRequestSchema = pairingModule.NmapModeRequestSchema;
  const PairRequestSchema = pairingModule.PairRequestSchema;

  // Setup transport
  const transport = createConnectTransport({
    baseUrl: "http://localhost:4000",
  });

  // Create clients
  const onboardingClient = createClient(OnboardingService, transport);
  const authClient = createClient(AuthService, transport);
  const pairingClient = createClient(PairingService, transport);

  // Step 0: Create admin user if it doesn't exist
  console.log("\n=== Step 0: Ensure Admin User Exists ===");

  // We'll attempt to create an admin user.
  // If the user already exists, the server will return an error,
  // which we'll catch and continue with authentication.
  try {
    console.log("Checking if admin user needs to be created...");
    const createAdminRequest = create(CreateAdminLoginRequestSchema, {
      username: "admin",
      password: "test1234",
    });

    const createAdminResponse =
      await onboardingClient.createAdminLogin(createAdminRequest);
    console.log("Admin user created successfully:", createAdminResponse.userId);
  } catch (error: any) {
    // Check if the error message indicates the user already exists
    if (error.message && error.message.includes("already exists")) {
      console.log("Admin user already exists, proceeding with authentication.");
    } else {
      console.log(
        "Error checking/creating admin user:",
        error.message || error,
      );
      // Continue anyway - we'll try to authenticate with the credentials
    }
  }

  // Step 1: Authenticate
  console.log("\n=== Step 1: Authentication ===");
  console.log("Authenticating with server...");

  try {
    // Create a properly formatted authentication request
    const authRequest = create(AuthenticateRequestSchema, {
      username: "admin",
      password: "test1234",
    });

    const authResponse = await authClient.authenticate(authRequest);
    console.log("Authentication successful, token received.");

    const token = authResponse.token;
    console.log(`Token: ${token}`, typeof token);
    const tokenExpiry = authResponse.tokenExpiry;
    console.log(
      `Token expires at: ${new Date(Number(tokenExpiry) * 1000).toISOString()}`,
    );

    // Set headers for subsequent requests
    const headers = {
      Authorization: `Bearer ${token}`,
    };

    // Step 2: Discover devices
    console.log("\n=== Step 2: Device Discovery ===");
    console.log("Discovering miners using different methods...");

    // Method 1: mDNS Discovery
    console.log("\nTrying mDNS discovery...");
    try {
      const mdnsModeRequest = create(MDNSModeRequestSchema, {
        serviceType: "_fleet._tcp",
        domain: "local",
        timeoutSeconds: 5,
      });

      const discoverRequest = create(DiscoverRequestSchema, {
        mode: {
          case: "mdns",
          value: mdnsModeRequest,
        },
      });

      // For server streaming, iterate through responses
      const stream = pairingClient.discover(discoverRequest, { headers });
      let deviceCount = 0;
      let foundDevices: any[] = [];

      for await (const response of stream) {
        if (response.devices && response.devices.length > 0) {
          deviceCount += response.devices.length;
          foundDevices = [...foundDevices, ...response.devices];
        }
      }

      if (deviceCount > 0) {
        console.log(`Found ${deviceCount} devices via mDNS.`);
      } else {
        console.log("No devices found via mDNS.");
      }
    } catch (error) {
      console.error("Error during mDNS discovery:", error);
    }

    // Method 2: IP List Scan
    console.log("\nTrying IP List scan...");
    try {
      const ipListModeRequest = create(IPListModeRequestSchema, {
        ipAddresses: ["192.168.2.10", "192.168.2.11"],
        ports: ["2121"],
        timeoutSeconds: 10,
      });

      const discoverRequest = create(DiscoverRequestSchema, {
        mode: {
          case: "ipList",
          value: ipListModeRequest,
        },
      });

      // For server streaming, iterate through responses
      const stream = pairingClient.discover(discoverRequest, { headers });
      let deviceCount = 0;
      let foundDevices: any[] = [];

      for await (const response of stream) {
        if (response.devices && response.devices.length > 0) {
          deviceCount += response.devices.length;
          foundDevices = [...foundDevices, ...response.devices];
        }
      }

      if (deviceCount > 0) {
        console.log(`Found ${deviceCount} devices via IP List scan.`);

        // Convert BigInt values to strings for safe JSON serialization
        const devicesForDisplay = foundDevices.map((device) => ({
          deviceIdentifier: device.deviceIdentifier,
          ipAddress: device.ipAddress,
          port: device.port,
          macAddress: device.macAddress,
          serialNumber: device.serialNumber,
          discoveredAt: String(device.discoveredAt),
        }));

        console.log(
          "Discovered devices:",
          JSON.stringify(devicesForDisplay, null, 2),
        );

        // Step 3: Pair with discovered devices
        console.log("\n=== Step 3: Device Pairing ===");
        const deviceIds = foundDevices.map((d) => d.deviceIdentifier);
        console.log(`Pairing with devices: ${deviceIds.join(", ")}`);

        try {
          const pairRequest = create(PairRequestSchema, {
            deviceIdentifiers: deviceIds,
          });

          const pairResponse = await pairingClient.pair(pairRequest, {
            headers,
          });
          console.log("Pairing successful:", pairResponse);
        } catch (error) {
          console.error("Error during pairing:", error);
        }
      } else {
        console.log("No devices found via IP List scan.");
      }
    } catch (error) {
      console.error("Error during IP List scan:", error);
    }

    // Method 3: Nmap Scan
    console.log("\nTrying Nmap scan...");
    try {
      const nmapModeRequest = create(NmapModeRequestSchema, {
        target: "192.168.2.0/24",
        ports: ["2121"],
        fastScan: false,
      });

      const discoverRequest = create(DiscoverRequestSchema, {
        mode: {
          case: "nmap",
          value: nmapModeRequest,
        },
      });

      // For server streaming, iterate through responses
      const stream = pairingClient.discover(discoverRequest, { headers });
      let deviceCount = 0;
      let foundDevices: any[] = [];

      for await (const response of stream) {
        if (response.devices && response.devices.length > 0) {
          deviceCount += response.devices.length;
          foundDevices = [...foundDevices, ...response.devices];
        }
      }

      if (deviceCount > 0) {
        console.log(`Found ${deviceCount} devices via Nmap scan.`);

        // Convert BigInt values to strings for safe JSON serialization
        const devicesForDisplay = foundDevices.map((device) => ({
          deviceIdentifier: device.deviceIdentifier,
          ipAddress: device.ipAddress,
          port: device.port,
          macAddress: device.macAddress,
          serialNumber: device.serialNumber,
          discoveredAt: String(device.discoveredAt),
        }));

        console.log(
          "Discovered devices:",
          JSON.stringify(devicesForDisplay, null, 2),
        );

        // Pair with discovered devices
        console.log("\n=== Additional Pairing via Nmap ===");
        const deviceIds = foundDevices.map((d) => d.deviceIdentifier);
        console.log(`Pairing with devices: ${deviceIds.join(", ")}`);

        try {
          const pairRequest = create(PairRequestSchema, {
            deviceIdentifiers: deviceIds,
          });

          const pairResponse = await pairingClient.pair(pairRequest, {
            headers,
          });
          console.log("Pairing successful:", pairResponse);
        } catch (error) {
          console.error("Error during pairing:", error);
        }
      } else {
        console.log("No devices found via Nmap scan.");
      }
    } catch (error) {
      console.error("Error during Nmap scan:", error);
    }

    // Step 4: Check paired miners
    console.log("\n=== Step 4: Listing Paired Miners ===");

    try {
      // First, we need to import the FleetManagement service
      const fleetModule = await import(
        "../src/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb"
      );
      const FleetManagementService = fleetModule.FleetManagementService;
      const ListPairedMinersRequestSchema =
        fleetModule.ListPairedMinersRequestSchema;

      // Create a client for the FleetManagement service
      const fleetClient = createClient(FleetManagementService, transport);

      // Create a request to list paired miners
      const listRequest = create(ListPairedMinersRequestSchema, {
        pageSize: 10,
      });

      console.log(listRequest);

      // Make the request
      const listResponse = await fleetClient.listPairedMiners(listRequest, {
        headers,
      });

      if (listResponse.miners && listResponse.miners.length > 0) {
        console.log(`Found ${listResponse.miners.length} paired miners:`);

        // Convert BigInt values to strings for safe JSON serialization
        const minersForDisplay = listResponse.miners.map((miner) => ({
          deviceIdentifier: miner.deviceIdentifier,
          macAddress: miner.macAddress,
          serialNumber: miner.serialNumber || "(no serial number)",
        }));

        console.log(JSON.stringify(minersForDisplay, null, 2));
      } else {
        console.log("No paired miners found.");
      }
    } catch (error) {
      console.error("Error listing paired miners:", error);
    }
  } catch (error) {
    console.error("Authentication failed:", error);
  }
}

main().catch(console.error);
