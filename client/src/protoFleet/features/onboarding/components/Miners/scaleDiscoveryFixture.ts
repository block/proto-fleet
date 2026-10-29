import { create } from "@bufbuild/protobuf";
import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";
import {
  FleetNodeEnrollmentStatus,
  FleetNodeHARole,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { DeviceSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

export const scaleFleetNodes: FleetNodeItem[] = Array.from({ length: 13 }, (_, index) => ({
  fleetNodeId: String(index + 1),
  pendingEnrollmentId: null,
  name: `Mining hall ${index + 1}`,
  enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
  identityFingerprint: "",
  commandProtocolUpgradeRequired: false,
  commandProtocolUpgradeRecommended: false,
  controlStreamConnected: true,
  discoveryCoverageDegraded: false,
  discoveryConfigReported: true,
  discoveryRanges: [`10.${index}.0.0/20`],
  pairedDeviceCount: index === 0 ? 1200 : index === 1 ? 200 : 0,
  haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_UNSPECIFIED,
  haPeerNodeId: null,
  haPeerNodeName: "",
  haStandbyReady: false,
  haStatusReason: "",
  ipAddresses: [],
  version: "",
  enrolledBy: "",
  createdAt: null,
  lastSeenAt: null,
}));

export function createScaleDiscoveryMiners() {
  return Array.from({ length: 50_000 }, (_, index) =>
    create(DeviceSchema, {
      deviceIdentifier: `miner-${index}`,
      ipAddress: `10.${Math.floor(index / 4000)}.${Math.floor((index % 4000) / 256)}.${index % 256}`,
      manufacturer: "Bitmain",
      model: index % 2 === 0 ? "S21 Pro" : "S19 Pro",
      deviceClass: DeviceClass.RIG,
      fleetNodeId: BigInt(Math.floor(index / 4000) + 1),
    }),
  );
}
