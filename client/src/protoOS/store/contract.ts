import type { Duration } from "@/shared/components/DurationSelector";
import type { TemperatureUnit, Theme, ThemeColor } from "@/shared/features/preferences/types";

// Nominal ("branded") type helper: tags a primitive with a phantom name so
// structurally-identical ids (DeviceId vs HashboardId, etc.) can't be swapped.
type Brand<T, Name extends string> = T & { readonly __brand: Name };

export type DeviceId = Brand<string, "DeviceId">;
export type ContainerId = Brand<string, "ContainerId">;
export type TankId = Brand<string, "TankId">;
export type ModuleId = Brand<string, "ModuleId">;
export type ComponentId = Brand<string, "ComponentId">;
export type HashboardId = Brand<string, "HashboardId">;
export type AsicId = Brand<string, "AsicId">;
export type PoolId = Brand<string, "PoolId">;
export type EntityKey = Brand<string, "EntityKey">;
export type ResourceKey = Brand<string, "ResourceKey">;

export type DeviceType = "rig" | "container";
/** Stable application-owned identifier for the API adapter selected for a miner. */
export type MinerAdapterId = "mdk-v1" | "mdk-v2";
export type ModuleModel = "r2" | "ct1" | "unknown";
export type CapabilitySupport = "supported" | "unsupported" | "unknown";

export interface CommandCapabilities {
  startStop: CapabilitySupport;
  powerTarget: CapabilitySupport;
  locate: CapabilitySupport;
  reboot: CapabilitySupport;
  cooling: CapabilitySupport;
  logs: CapabilitySupport;
  firmwareUpdate: CapabilitySupport;
  poolEditing: CapabilitySupport;
  passwordManagement: CapabilitySupport;
}

export interface ModuleCapabilities {
  commands: CommandCapabilities;
}

export type ViewSelection =
  | { level: "rig"; moduleId: ModuleId }
  | { level: "container"; containerId: ContainerId }
  | { level: "tank"; tankId: TankId }
  | { level: "module"; moduleId: ModuleId };

export type EntityRef =
  | { kind: "device"; id: DeviceId }
  | { kind: "container"; id: ContainerId }
  | { kind: "tank"; id: TankId }
  | { kind: "module"; id: ModuleId }
  | { kind: "hashboard"; id: HashboardId };

export type ComponentRef =
  | { id: ComponentId; kind: "controlBoard"; owner: EntityRef }
  | { id: ComponentId; kind: "psu"; owner: EntityRef }
  | { id: ComponentId; kind: "fan"; owner: EntityRef }
  | { id: ComponentId; kind: "plant"; owner: EntityRef }
  | { id: HashboardId; kind: "hashboard"; owner: EntityRef }
  | { id: AsicId; kind: "asic"; owner: EntityRef };

export type ControlBoardRef = Extract<ComponentRef, { kind: "controlBoard" }>;
export type HashboardRef = Extract<ComponentRef, { kind: "hashboard" }>;
export type AsicRef = Extract<ComponentRef, { kind: "asic" }>;
export type PsuRef = Extract<ComponentRef, { kind: "psu" }>;
export type FanRef = Extract<ComponentRef, { kind: "fan" }>;
export type PlantComponentRef = Extract<ComponentRef, { kind: "plant" }>;

export type BoardIdentity =
  { kind: "serial"; value: string } | { kind: "mac"; value: string } | { kind: "unknown"; value: string };

export interface StoreError {
  code: string;
  message: string;
  retryable: boolean;
}

export type ResourceState =
  | { status: "idle" | "loading"; updatedAtMs?: number }
  | { status: "ready" | "stale"; updatedAtMs: number }
  | { status: "unsupported"; updatedAtMs?: number; reason?: string }
  | { status: "error"; updatedAtMs?: number; error: StoreError };

export type MetricUnit =
  | "TH/s"
  | "GH/s"
  | "°C"
  | "W"
  | "J/TH"
  | "%"
  | "RPM"
  | "V"
  | "A"
  | "VA"
  | "var"
  | "kWh"
  | "kVAh"
  | "kvarh"
  | "Pa"
  | "L/min"
  | "mm"
  | "Hz"
  | "MHz"
  | "s"
  | "ms"
  | "ratio";

export interface Measurement {
  value: number | null;
  units: MetricUnit;
}

export interface MetricPoint {
  timestampMs: number;
  value: number | null;
  min?: number | null;
  max?: number | null;
  sampleCount?: number;
}

export interface MetricTimeSeries {
  points: MetricPoint[];
  bucketSizeMs?: number;
  units?: MetricUnit;
  aggregates?: { min?: Measurement; avg?: Measurement; max?: Measurement };
}

export interface MetricTelemetry {
  latest?: Measurement;
  timeSeries?: MetricTimeSeries;
}

export interface RigTopology {
  kind: "rig";
  deviceId: DeviceId;
  capabilities: CommandCapabilities;
  rootModuleId: ModuleId;
  modules: Map<ModuleId, ModuleNode>;
  hashboards: Map<HashboardId, HashboardNode>;
}

export interface ContainerTopology {
  kind: "container";
  deviceId: DeviceId;
  containerId: ContainerId;
  container: ContainerNode;
  tanks: Map<TankId, TankNode>;
  modules: Map<ModuleId, ModuleNode>;
  hashboards: Map<HashboardId, HashboardNode>;
}

export interface DirectModuleTopology {
  kind: "directModule";
  deviceId: DeviceId;
  moduleId: ModuleId;
  modules: Map<ModuleId, ModuleNode>;
  hashboards: Map<HashboardId, HashboardNode>;
}

export type Topology = RigTopology | ContainerTopology | DirectModuleTopology;

export interface ContainerNode {
  id: ContainerId;
  capabilities: CommandCapabilities;
  tankIds: TankId[];
  controlBoardIds: ComponentId[];
  fanIds: ComponentId[];
  plantComponentIds: ComponentId[];
}

export interface TankNode {
  id: TankId;
  containerId: ContainerId;
  capabilities: CommandCapabilities;
  moduleIds: ModuleId[];
  label?: string;
  slot?: number;
}

export interface ModuleNode {
  id: ModuleId;
  profile: ModuleModel;
  capabilities: ModuleCapabilities;
  tankId?: TankId;
  hashboardIds: HashboardId[];
  controlBoardIds: ComponentId[];
  psuIds: ComponentId[];
  fanIds: ComponentId[];
  location?: { tankLabel?: string; moduleSlot?: number; bay?: number };
}

export interface HashboardNode {
  id: HashboardId;
  identity: BoardIdentity;
  moduleId: ModuleId;
  asicIds: AsicId[];
  location?: { slot?: number; bay?: number; port?: number };
  breakerId?: string;
  powerFeedId?: string;
}

export interface FirmwareInfo {
  name?: string;
  version?: string;
  variant?: string;
  build?: "debug" | "release" | "manufacturing" | "unknown";
  gitHash?: string;
  imageHash?: string;
}

export interface ControlBoardHardware {
  ref: ControlBoardRef;
  role?: "primary" | "secondary" | "unknown";
  serialNumber?: string;
  boardId?: string;
  machineName?: string;
  firmware?: FirmwareInfo;
  cpu?: {
    modelName?: string;
    architecture?: string;
    hardware?: string;
    revision?: string;
    implementer?: string;
    part?: string;
    variant?: string;
  };
}

export interface HashboardHardware {
  ref: HashboardRef;
  identity: BoardIdentity;
  boardModel?: string;
  apiVersion?: string;
  chipId?: string;
  miningAsic?: "BZM" | "MC1" | "MC2" | "MC3" | "unknown";
  miningAsicCount?: number;
  temperatureSensorCount?: number;
  ecLogsPath?: string;
  firmware?: FirmwareInfo;
  bootloader?: FirmwareInfo;
}

export interface AsicHardware {
  ref: AsicRef;
  hashboardId: HashboardId;
  index?: number;
  row?: number;
  column?: number;
}

export interface PsuHardware {
  ref: PsuRef;
  slot?: number;
  serialNumber?: string;
  manufacturer?: string;
  model?: string;
  hardwareRevision?: string;
  firmware?: { application?: FirmwareInfo; bootloader?: FirmwareInfo };
}

export interface FanHardware {
  ref: FanRef;
  slot?: number;
  name?: string;
}

export interface HardwareState {
  resource: ResourceState;
  topology?: Topology;
  controlBoards: Map<ComponentId, ControlBoardHardware>;
  hashboards: Map<HashboardId, HashboardHardware>;
  asics: Map<AsicId, AsicHardware>;
  psus: Map<ComponentId, PsuHardware>;
  fans: Map<ComponentId, FanHardware>;
}

export interface LevelTelemetry {
  hashrate?: MetricTelemetry;
  power?: MetricTelemetry;
  efficiency?: MetricTelemetry;
  temperature?: MetricTelemetry;
}

export type HealthState = "ok" | "degraded" | "fault" | "offline" | "unknown";
export type MiningStatus =
  | "uninitialized"
  | "poweringOn"
  | "mining"
  | "degradedMining"
  | "poweringOff"
  | "stopped"
  | "curtailed"
  | "noPools"
  | "error"
  | "unknown";

export interface PowerCoverage {
  kind: "complete" | "partial" | "hashboardOnly" | "unknown";
  includesNonHashboardLoads: boolean;
  methods: ("firmwareAggregate" | "topologyMapping" | "allocationPolicy" | "unattributed")[];
  unattributedPower?: Measurement;
}

export interface LevelRollup extends LevelTelemetry {
  miningStatus?: MiningStatus;
  health?: HealthState;
  boardCount?: number;
  onlineBoardCount?: number;
  powerCoverage?: PowerCoverage;
}

export interface HashboardTelemetry extends LevelTelemetry {
  hashboardId: HashboardId;
  inletTemperature?: MetricTelemetry;
  outletTemperature?: MetricTelemetry;
  averageAsicTemperature?: MetricTelemetry;
  maximumAsicTemperature?: MetricTelemetry;
}

export interface AsicTelemetry {
  asicId: AsicId;
  hashrate?: MetricTelemetry;
  temperature?: MetricTelemetry;
  voltage?: MetricTelemetry;
  frequency?: MetricTelemetry;
}

export interface PsuTelemetry {
  componentId: ComponentId;
  inputVoltage?: MetricTelemetry;
  inputCurrent?: MetricTelemetry;
  inputPower?: MetricTelemetry;
  outputVoltage?: MetricTelemetry;
  outputCurrent?: MetricTelemetry;
  outputPower?: MetricTelemetry;
  ambientTemperature?: MetricTelemetry;
  averageTemperature?: MetricTelemetry;
  hotspotTemperature?: MetricTelemetry;
}

export interface FanTelemetry {
  componentId: ComponentId;
  rpm?: MetricTelemetry;
  speedPercent?: MetricTelemetry;
  minimumRpm?: MetricTelemetry;
  maximumRpm?: MetricTelemetry;
  health?: HealthState;
}

export type CoolingMode = "unknown" | "off" | "automatic" | "manual";

export interface TelemetryState {
  resourceByScope: Map<ResourceKey, ResourceState>;
  rollupsByEntity: Map<EntityKey, LevelRollup>;
  hashboards: Map<HashboardId, HashboardTelemetry>;
  asics: Map<AsicId, AsicTelemetry>;
  psus: Map<ComponentId, PsuTelemetry>;
  fans: Map<ComponentId, FanTelemetry>;
  coolingModeByOwner: Map<EntityKey, CoolingMode>;
  asicLoadByHashboard: Map<HashboardId, ResourceState>;
}

export interface DeviceIdentity {
  deviceId: DeviceId;
  deviceType: DeviceType;
  adapterId: MinerAdapterId;
  productName?: string;
  model?: string;
  manufacturer?: string;
  serialNumber?: string;
  macAddresses: string[];
}

export type ServiceHealth = "running" | "stopped" | "starting" | "error" | "unknown";
export type FirmwareUpdateStatus =
  | "unknown"
  | "current"
  | "available"
  | "downloading"
  | "downloaded"
  | "installing"
  | "confirming"
  | "rebootRequired"
  | "complete"
  | "failed";

export interface FirmwareUpdateInfo {
  status: FirmwareUpdateStatus;
  currentVersion?: string;
  newVersion?: string;
  progressPercent?: number;
  message?: string;
  releaseNotes?: string;
}

export interface SystemInfoData {
  identity: DeviceIdentity;
  moduleModel: ModuleModel;
  operatingSystem?: FirmwareInfo;
  operatingSystemStatus?: {
    cpuLoadPercent?: number;
    memoryFreeKb?: number;
    memoryTotalKb?: number;
    rootFilesystemFreeMb?: number;
    rootFilesystemTotalMb?: number;
  };
  controlBoardSoftware?: FirmwareInfo;
  webServer?: { health: ServiceHealth; software?: FirmwareInfo };
  miningDriver?: { health: ServiceHealth; software?: FirmwareInfo };
  poolInterface?: { health: ServiceHealth; software?: FirmwareInfo };
  firmwareUpdate?: FirmwareUpdateInfo;
  uptimeSeconds?: number;
}

export interface SystemInfoState {
  resource: ResourceState;
  data?: SystemInfoData;
  systemTag?: string;
}

export interface NetworkInterface {
  id: string;
  name?: string;
  hostname?: string;
  ipAddress?: string;
  macAddress?: string;
  gateway?: string;
  netmask?: string;
  dhcp?: boolean;
  isPrimary: boolean;
}

export interface NetworkInfoState {
  resource: ResourceState;
  interfaces: Map<string, NetworkInterface>;
}

export interface PoolInfo {
  id: PoolId;
  name?: string;
  url?: string;
  username?: string;
  priority: number;
  status?: string;
  acceptedShares?: number;
  rejectedShares?: number;
  invalidShares?: number;
  difficulty?: number;
  lastShareAtMs?: number;
  bestShare?: number;
  blocks?: number;
}

export interface PoolsState {
  resourceByOwner: Map<EntityKey, ResourceState>;
  byOwner: Map<EntityKey, PoolInfo[]>;
}

export interface OperationalStatus {
  owner: EntityRef;
  resource: ResourceState;
  miningStatus: MiningStatus;
  miningUptime?: Measurement;
  rebootUptime?: Measurement;
  hardwareErrorCount?: number;
  message?: string;
}

export interface ErrorSourceLocator {
  kind: "rig" | "fan" | "psu" | "hashboard";
  slot?: number;
}

export interface MinerError {
  id: string;
  code: string;
  message: string;
  owner: EntityRef;
  source?: ErrorSourceLocator;
  component?: ComponentRef;
  severity: "info" | "warning" | "critical";
  timestampMs?: number;
}

export interface DeviceLifecycle {
  resource: ResourceState;
  onboarded?: boolean;
  passwordSet?: boolean;
  defaultPasswordActive?: boolean;
}

export interface MinerStatusState {
  operationalByOwner: Map<EntityKey, OperationalStatus>;
  errorsByOwner: Map<EntityKey, MinerError[]>;
  lifecycle: DeviceLifecycle;
}

export type PerformanceMode = "maximumHashrate" | "efficiency";

export interface MiningTarget {
  moduleId: ModuleId;
  resource: ResourceState;
  valueWatts?: number;
  defaultWatts?: number;
  performanceMode?: PerformanceMode;
  boundsWatts?: { min: number; max: number };
  balanceBays?: boolean;
  hashOnDisconnect?: boolean;
}

export interface MiningTargetState {
  byModule: Map<ModuleId, MiningTarget>;
}

export type MiningTargetUpdate = Pick<
  MiningTarget,
  "valueWatts" | "performanceMode" | "balanceBays" | "hashOnDisconnect"
>;

export interface AuthToken {
  value: string;
  expiresAtMs: number;
}

export type AuthState =
  | { mode: "bearer"; status: "loading" | "unauthenticated"; accessToken?: undefined; refreshToken?: undefined }
  | { mode: "bearer"; status: "authenticated" | "refreshing"; accessToken: AuthToken; refreshToken: AuthToken }
  | { mode: "bearer"; status: "error"; accessToken?: AuthToken; refreshToken?: AuthToken; error: StoreError }
  | { mode: "actorHeader"; status: "ready"; actor?: string }
  | { mode: "fleetProxy"; status: "delegated" };

export interface UIState {
  duration: Duration;
  activeChartLines: string[];
  theme: Theme;
  deviceTheme: Theme;
  temperatureUnit: TemperatureUnit;
  themeColor?: ThemeColor;
  firmwareUpdateDismissed: boolean;
  showLoginModal: boolean;
  dismissedLoginModal: boolean;
  pausedAuthAction: string | null;
}

export interface StoreWriteToken {
  connectionGeneration: number;
  requestId: number;
}

export const ROOT_MODULE_ID = "module:root" as ModuleId;
export const ROOT_MODULE_KEY = "module:root" as EntityKey;

export const asDeviceId = (value: string) => value as DeviceId;
export const asModuleId = (value: string) => value as ModuleId;
export const asComponentId = (value: string) => value as ComponentId;
export const asHashboardId = (value: string) => value as HashboardId;
export const asAsicId = (value: string) => value as AsicId;
export const asEntityKey = (value: string) => value as EntityKey;
export const asResourceKey = (value: string) => value as ResourceKey;
export const asPoolId = (value: string) => value as PoolId;
