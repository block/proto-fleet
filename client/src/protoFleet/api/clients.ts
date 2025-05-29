import { createClient } from "@connectrpc/connect";
import { AuthService } from "./generated/auth/v1/auth_pb";
import { FleetManagementService } from "./generated/fleetmanagement/v1/fleetmanagement_pb";
import { OnboardingService } from "./generated/onboarding/v1/onboarding_pb";
import { transport } from "./transport";
import { MinerCommandService } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { NetworkInfoService } from "@/protoFleet/api/generated/networkinfo/v1/networkinfo_pb";
import { PairingService } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { PoolsService } from "@/protoFleet/api/generated/pools/v1/pools_pb";

const authClient = createClient(AuthService, transport);
const networkInfoClient = createClient(NetworkInfoService, transport);
const pairingClient = createClient(PairingService, transport);
const fleetManagementClient = createClient(FleetManagementService, transport);
const onboardingClient = createClient(OnboardingService, transport);
const minerCommandClient = createClient(MinerCommandService, transport);
const poolsClient = createClient(PoolsService, transport);
export {
  authClient,
  networkInfoClient,
  pairingClient,
  fleetManagementClient,
  onboardingClient,
  minerCommandClient,
  poolsClient,
};
