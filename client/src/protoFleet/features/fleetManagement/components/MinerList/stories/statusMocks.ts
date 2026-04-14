/**
 * Comprehensive mock data showing all possible miner statuses and issues
 * for Storybook visual verification
 */

import { create } from "@bufbuild/protobuf";
import {
  type MinerCapabilities,
  MinerCapabilitiesSchema,
  type TelemetryCapabilities,
  TelemetryCapabilitiesSchema,
} from "@/protoFleet/api/generated/capabilities/v1/capabilities_pb";
import { type Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import {
  ComponentType,
  ErrorMessageSchema,
  MinerError,
  Severity,
} from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { type ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import {
  DeviceStatus,
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { TemperatureStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

// Shared capabilities - all miners support all telemetry metrics
const baseTelemetryCapabilities: TelemetryCapabilities = create(TelemetryCapabilitiesSchema, {
  realtimeTelemetrySupported: true,
  historicalDataSupported: true,
  hashrateReported: true,
  powerUsageReported: true,
  temperatureReported: true,
  fanSpeedReported: true,
  efficiencyReported: true,
  uptimeReported: true,
  errorCountReported: true,
  minerStatusReported: true,
  poolStatsReported: true,
  perChipStatsReported: false,
  perBoardStatsReported: false,
  psuStatsReported: false,
});

const baseCapabilities: MinerCapabilities = create(MinerCapabilitiesSchema, {
  manufacturer: "Bitmain",
  telemetry: baseTelemetryCapabilities,
});

// Shared measurement data
const baseMeasurements = {
  hashrate: [
    {
      timestamp: { seconds: BigInt(1641283200), nanos: 0 },
      value: 100.0,
    } as Measurement,
  ],
  efficiency: [
    {
      timestamp: { seconds: BigInt(2), nanos: 0 },
      value: 15.5,
    } as Measurement,
  ],
  powerUsage: [
    {
      timestamp: { seconds: BigInt(2), nanos: 0 },
      value: 3.5,
    } as Measurement,
  ],
  temperature: [
    {
      timestamp: { seconds: BigInt(2), nanos: 0 },
      value: 65.5,
    } as Measurement,
  ],
  workerName: "worker-base",
  groupLabels: [] as string[],
  rackLabel: "",
  rackPosition: "",
};

// ============================================================================
// Status Examples
// ============================================================================

export const hashingMiner: MinerStateSnapshot = {
  $typeName: "fleetmanagement.v1.MinerStateSnapshot",
  deviceIdentifier: "uuid:status-hashing",
  serialNumber: "SN-HASHING",
  name: "Hashing Miner",
  ipAddress: "192.168.1.101",
  macAddress: "0a:00:00:00:00:01",
  url: "https://192.168.1.101:8080",
  pairingStatus: PairingStatus.PAIRED,
  model: "S19 Pro",
  manufacturer: "Bitmain",
  driverName: "antminer",
  ...baseMeasurements,
  deviceStatus: DeviceStatus.ONLINE,
  temperatureStatus: TemperatureStatus.OK,
  firmwareVersion: "2.0.0",
  capabilities: baseCapabilities,
};

export const offlineMiner: MinerStateSnapshot = {
  $typeName: "fleetmanagement.v1.MinerStateSnapshot",
  deviceIdentifier: "uuid:status-offline",
  serialNumber: "SN-OFFLINE",
  name: "Offline Miner",
  ipAddress: "192.168.1.102",
  macAddress: "0a:00:00:00:00:02",
  url: "https://192.168.1.102:8080",
  pairingStatus: PairingStatus.PAIRED,
  model: "S19 Pro",
  manufacturer: "Bitmain",
  workerName: "worker-offline",
  driverName: "antminer",
  hashrate: [],
  efficiency: [],
  powerUsage: [],
  temperature: [],
  deviceStatus: DeviceStatus.OFFLINE,
  temperatureStatus: TemperatureStatus.OK,
  firmwareVersion: "2.0.0",
  capabilities: baseCapabilities,
  groupLabels: [],
  rackLabel: "",
  rackPosition: "",
};

export const sleepingMiner: MinerStateSnapshot = {
  $typeName: "fleetmanagement.v1.MinerStateSnapshot",
  deviceIdentifier: "uuid:status-sleeping",
  serialNumber: "SN-SLEEPING",
  name: "Sleeping Miner",
  ipAddress: "192.168.1.103",
  macAddress: "0a:00:00:00:00:03",
  url: "https://192.168.1.103:8080",
  pairingStatus: PairingStatus.PAIRED,
  model: "S19 Pro",
  manufacturer: "Bitmain",
  workerName: "worker-sleeping",
  driverName: "antminer",
  hashrate: [
    {
      timestamp: { seconds: BigInt(1641283200), nanos: 0 },
      value: 0,
    } as Measurement,
  ],
  efficiency: [],
  powerUsage: [],
  temperature: baseMeasurements.temperature,
  deviceStatus: DeviceStatus.INACTIVE,
  temperatureStatus: TemperatureStatus.OK,
  firmwareVersion: "2.0.0",
  capabilities: baseCapabilities,
  groupLabels: [],
  rackLabel: "",
  rackPosition: "",
};

// ============================================================================
// Issue Examples - Simple Issues
// ============================================================================

export const authRequiredMiner: MinerStateSnapshot = {
  $typeName: "fleetmanagement.v1.MinerStateSnapshot",
  deviceIdentifier: "uuid:issue-auth",
  serialNumber: "SN-AUTH",
  name: "Auth Required",
  ipAddress: "192.168.1.110",
  macAddress: "0a:00:00:00:00:10",
  url: "https://192.168.1.110:8080",
  pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
  model: "S19 Pro",
  manufacturer: "Bitmain",
  workerName: "worker-auth",
  driverName: "antminer",
  hashrate: [],
  efficiency: [],
  powerUsage: [],
  temperature: [],
  deviceStatus: DeviceStatus.ERROR,
  temperatureStatus: TemperatureStatus.OK,
  firmwareVersion: "2.0.0",
  capabilities: baseCapabilities,
  groupLabels: [],
  rackLabel: "",
  rackPosition: "",
};

export const poolRequiredMiner: MinerStateSnapshot = {
  $typeName: "fleetmanagement.v1.MinerStateSnapshot",
  deviceIdentifier: "uuid:issue-pool",
  serialNumber: "SN-POOL",
  name: "Pool Required",
  ipAddress: "192.168.1.111",
  macAddress: "0a:00:00:00:00:11",
  url: "https://192.168.1.111:8080",
  pairingStatus: PairingStatus.PAIRED,
  model: "S19 Pro",
  manufacturer: "Bitmain",
  driverName: "antminer",
  ...baseMeasurements,
  deviceStatus: DeviceStatus.NEEDS_MINING_POOL,
  temperatureStatus: TemperatureStatus.OK,
  firmwareVersion: "2.0.0",
  capabilities: baseCapabilities,
};

export const controlBoardFailureMiner: MinerStateSnapshot = {
  $typeName: "fleetmanagement.v1.MinerStateSnapshot",
  deviceIdentifier: "uuid:issue-controlboard",
  serialNumber: "SN-CTRLBOARD",
  name: "Control Board Issue",
  ipAddress: "192.168.1.112",
  macAddress: "0a:00:00:00:00:12",
  url: "https://192.168.1.112:8080",
  pairingStatus: PairingStatus.PAIRED,
  model: "S19 Pro",
  manufacturer: "Bitmain",
  driverName: "antminer",
  ...baseMeasurements,
  deviceStatus: DeviceStatus.ERROR,
  temperatureStatus: TemperatureStatus.OK,
  firmwareVersion: "2.0.0",
  capabilities: baseCapabilities,
};

export const hashboardFailureMiner: MinerStateSnapshot = {
  $typeName: "fleetmanagement.v1.MinerStateSnapshot",
  deviceIdentifier: "uuid:issue-hashboard",
  serialNumber: "SN-HASHBOARD",
  name: "Hashboard Issue",
  ipAddress: "192.168.1.113",
  macAddress: "0a:00:00:00:00:13",
  url: "https://192.168.1.113:8080",
  pairingStatus: PairingStatus.PAIRED,
  model: "S19 Pro",
  manufacturer: "Bitmain",
  driverName: "antminer",
  ...baseMeasurements,
  deviceStatus: DeviceStatus.ERROR,
  temperatureStatus: TemperatureStatus.OK,
  firmwareVersion: "2.0.0",
  capabilities: baseCapabilities,
};

export const psuFailureMiner: MinerStateSnapshot = {
  $typeName: "fleetmanagement.v1.MinerStateSnapshot",
  deviceIdentifier: "uuid:issue-psu",
  serialNumber: "SN-PSU",
  name: "PSU Issue",
  ipAddress: "192.168.1.114",
  macAddress: "0a:00:00:00:00:14",
  url: "https://192.168.1.114:8080",
  pairingStatus: PairingStatus.PAIRED,
  model: "S19 Pro",
  manufacturer: "Bitmain",
  driverName: "antminer",
  ...baseMeasurements,
  deviceStatus: DeviceStatus.ERROR,
  temperatureStatus: TemperatureStatus.OK,
  firmwareVersion: "2.0.0",
  capabilities: baseCapabilities,
};

export const fanFailureMiner: MinerStateSnapshot = {
  $typeName: "fleetmanagement.v1.MinerStateSnapshot",
  deviceIdentifier: "uuid:issue-fan",
  serialNumber: "SN-FAN",
  name: "Fan Issue",
  ipAddress: "192.168.1.115",
  macAddress: "0a:00:00:00:00:15",
  url: "https://192.168.1.115:8080",
  pairingStatus: PairingStatus.PAIRED,
  model: "S19 Pro",
  manufacturer: "Bitmain",
  driverName: "antminer",
  ...baseMeasurements,
  deviceStatus: DeviceStatus.ERROR,
  temperatureStatus: TemperatureStatus.OK,
  firmwareVersion: "2.0.0",
  capabilities: baseCapabilities,
};

// ============================================================================
// Issue Examples - Multiple Failures
// ============================================================================

export const multipleHashboardFailuresMiner: MinerStateSnapshot = {
  $typeName: "fleetmanagement.v1.MinerStateSnapshot",
  deviceIdentifier: "uuid:issue-multiple-hashboards",
  serialNumber: "SN-MULTI-HB",
  name: "Multiple Hashboards",
  ipAddress: "192.168.1.120",
  macAddress: "0a:00:00:00:00:20",
  url: "https://192.168.1.120:8080",
  pairingStatus: PairingStatus.PAIRED,
  model: "S19 Pro",
  manufacturer: "Bitmain",
  driverName: "antminer",
  ...baseMeasurements,
  deviceStatus: DeviceStatus.ERROR,
  temperatureStatus: TemperatureStatus.OK,
  firmwareVersion: "2.0.0",
  capabilities: baseCapabilities,
};

export const multipleComponentFailuresMiner: MinerStateSnapshot = {
  $typeName: "fleetmanagement.v1.MinerStateSnapshot",
  deviceIdentifier: "uuid:issue-multiple-components",
  serialNumber: "SN-MULTI-COMP",
  name: "Multiple Components",
  ipAddress: "192.168.1.121",
  macAddress: "0a:00:00:00:00:21",
  url: "https://192.168.1.121:8080",
  pairingStatus: PairingStatus.PAIRED,
  model: "S19 Pro",
  manufacturer: "Bitmain",
  driverName: "antminer",
  ...baseMeasurements,
  deviceStatus: DeviceStatus.ERROR,
  temperatureStatus: TemperatureStatus.OK,
  firmwareVersion: "2.0.0",
  capabilities: baseCapabilities,
};

// ============================================================================
// Error Messages (to be added to normalized error store)
// ============================================================================

export const errorMessages: ErrorMessage[] = [
  // Control board error
  create(ErrorMessageSchema, {
    errorId: "error-controlboard-1",
    deviceIdentifier: "uuid:issue-controlboard",
    componentType: ComponentType.CONTROL_BOARD,
    componentId: "1",
    summary: "Control board failure detected",
    canonicalError: MinerError.UNSPECIFIED,
    severity: Severity.MAJOR,
    causeSummary: "",
    recommendedAction: "",
    impact: "",
    vendorAttributes: {},
  }),
  // Single hashboard error
  create(ErrorMessageSchema, {
    errorId: "error-hashboard-1",
    deviceIdentifier: "uuid:issue-hashboard",
    componentType: ComponentType.HASH_BOARD,
    componentId: "1",
    summary: "Hashboard 1 failure",
    canonicalError: MinerError.UNSPECIFIED,
    severity: Severity.MAJOR,
    causeSummary: "",
    recommendedAction: "",
    impact: "",
    vendorAttributes: {},
  }),
  // PSU error
  create(ErrorMessageSchema, {
    errorId: "error-psu-1",
    deviceIdentifier: "uuid:issue-psu",
    componentType: ComponentType.PSU,
    componentId: "1",
    summary: "PSU 1 failure",
    canonicalError: MinerError.UNSPECIFIED,
    severity: Severity.MAJOR,
    causeSummary: "",
    recommendedAction: "",
    impact: "",
    vendorAttributes: {},
  }),
  // Fan error
  create(ErrorMessageSchema, {
    errorId: "error-fan-1",
    deviceIdentifier: "uuid:issue-fan",
    componentType: ComponentType.FAN,
    componentId: "1",
    summary: "Fan 1 failure",
    canonicalError: MinerError.UNSPECIFIED,
    severity: Severity.MAJOR,
    causeSummary: "",
    recommendedAction: "",
    impact: "",
    vendorAttributes: {},
  }),
  // Multiple hashboard errors (same component type)
  create(ErrorMessageSchema, {
    errorId: "error-hashboard-2",
    deviceIdentifier: "uuid:issue-multiple-hashboards",
    componentType: ComponentType.HASH_BOARD,
    componentId: "1",
    summary: "Hashboard 1 failure",
    canonicalError: MinerError.UNSPECIFIED,
    severity: Severity.MAJOR,
    causeSummary: "",
    recommendedAction: "",
    impact: "",
    vendorAttributes: {},
  }),
  create(ErrorMessageSchema, {
    errorId: "error-hashboard-3",
    deviceIdentifier: "uuid:issue-multiple-hashboards",
    componentType: ComponentType.HASH_BOARD,
    componentId: "2",
    summary: "Hashboard 2 failure",
    canonicalError: MinerError.UNSPECIFIED,
    severity: Severity.MAJOR,
    causeSummary: "",
    recommendedAction: "",
    impact: "",
    vendorAttributes: {},
  }),
  // Multiple component type errors
  create(ErrorMessageSchema, {
    errorId: "error-multi-hashboard",
    deviceIdentifier: "uuid:issue-multiple-components",
    componentType: ComponentType.HASH_BOARD,
    componentId: "1",
    summary: "Hashboard 1 failure",
    canonicalError: MinerError.UNSPECIFIED,
    severity: Severity.MAJOR,
    causeSummary: "",
    recommendedAction: "",
    impact: "",
    vendorAttributes: {},
  }),
  create(ErrorMessageSchema, {
    errorId: "error-multi-fan",
    deviceIdentifier: "uuid:issue-multiple-components",
    componentType: ComponentType.FAN,
    componentId: "1",
    summary: "Fan 1 failure",
    canonicalError: MinerError.UNSPECIFIED,
    severity: Severity.MAJOR,
    causeSummary: "",
    recommendedAction: "",
    impact: "",
    vendorAttributes: {},
  }),
  create(ErrorMessageSchema, {
    errorId: "error-multi-psu",
    deviceIdentifier: "uuid:issue-multiple-components",
    componentType: ComponentType.PSU,
    componentId: "1",
    summary: "PSU 1 failure",
    canonicalError: MinerError.UNSPECIFIED,
    severity: Severity.MAJOR,
    causeSummary: "",
    recommendedAction: "",
    impact: "",
    vendorAttributes: {},
  }),
];

// All status miners
export const allStatusMiners: MinerStateSnapshot[] = [hashingMiner, offlineMiner, sleepingMiner];

// All issue miners
export const allIssueMiners: MinerStateSnapshot[] = [
  authRequiredMiner,
  poolRequiredMiner,
  controlBoardFailureMiner,
  hashboardFailureMiner,
  psuFailureMiner,
  fanFailureMiner,
  multipleHashboardFailuresMiner,
  multipleComponentFailuresMiner,
];

// All miners combined
export const allMiners: MinerStateSnapshot[] = [...allStatusMiners, ...allIssueMiners];
