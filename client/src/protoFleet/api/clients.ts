import { createClient } from "@connectrpc/connect";
import { transport } from "./transport";
import { ActivityService } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { AuthService } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { DeviceCollectionService } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import { ErrorQueryService } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { FleetManagementService } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { MinerCommandService } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { NetworkInfoService } from "@/protoFleet/api/generated/networkinfo/v1/networkinfo_pb";
import { OnboardingService } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import { PairingService } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { PoolsService } from "@/protoFleet/api/generated/pools/v1/pools_pb";
import { TelemetryService } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

const activityClient = createClient(ActivityService, transport);
const authClient = createClient(AuthService, transport);
const errorQueryClient = createClient(ErrorQueryService, transport);
const networkInfoClient = createClient(NetworkInfoService, transport);
const pairingClient = createClient(PairingService, transport);
const fleetManagementClient = createClient(FleetManagementService, transport);
const onboardingClient = createClient(OnboardingService, transport);
const minerCommandClient = createClient(MinerCommandService, transport);
const poolsClient = createClient(PoolsService, transport);
const collectionClient = createClient(DeviceCollectionService, transport);
const telemetryClient = createClient(TelemetryService, transport);

export {
  activityClient,
  authClient,
  collectionClient,
  errorQueryClient,
  networkInfoClient,
  pairingClient,
  fleetManagementClient,
  onboardingClient,
  minerCommandClient,
  poolsClient,
  telemetryClient,
};
