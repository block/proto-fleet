/* eslint-disable */
/* tslint:disable */
// @ts-nocheck
/*
 * ---------------------------------------------------------------
 * ## THIS FILE WAS GENERATED VIA SWAGGER-TYPESCRIPT-API        ##
 * ##                                                           ##
 * ## AUTHOR: acacode                                           ##
 * ## SOURCE: https://github.com/acacode/swagger-typescript-api ##
 * ---------------------------------------------------------------
 */

export interface AlertServiceAcknowledgeAlertParams {
  /** @format int64 */
  id: string;
}

export interface AlertServiceListAlertsParams {
  /** @format int32 */
  "page.pageSize"?: number;
  "page.pageToken"?: string;
  acknowledged?: boolean;
  category?: string;
  /** @default "SEVERITY_UNSPECIFIED" */
  severity?:
    | "SEVERITY_UNSPECIFIED"
    | "SEVERITY_INFO"
    | "SEVERITY_WARNING"
    | "SEVERITY_ERROR"
    | "SEVERITY_CRITICAL";
}

export interface AlertServiceStreamAlertsParams {
  /** @default "SEVERITY_UNSPECIFIED" */
  minSeverity?:
    | "SEVERITY_UNSPECIFIED"
    | "SEVERITY_INFO"
    | "SEVERITY_WARNING"
    | "SEVERITY_ERROR"
    | "SEVERITY_CRITICAL";
}

export interface AuthServiceLogoutParams {
  /**
   * token optionally identifies the session for native gRPC callers. An empty
   * value uses bearer authentication metadata. The REST binding does not map
   * this field into a request body.
   */
  token?: string;
}

export interface FeedMappingServiceApplySoftFeedMappingRunBody {
  confirmApply?: boolean;
}

export interface FeedMappingServiceApplySoftFeedMappingRunParams {
  apply: string;
  runId: string;
}

export type FeedMappingServiceCancelFeedMappingRunBody = object;

export interface FeedMappingServiceCancelFeedMappingRunParams {
  cancel: string;
  runId: string;
}

export type FeedMappingServiceCancelSoftFeedMappingRunBody = object;

export interface FeedMappingServiceCancelSoftFeedMappingRunParams {
  cancel: string;
  runId: string;
}

export interface FeedMappingServiceGetFeedMappingReportParams {
  /**
   * Maximum recent changes to include. The server defaults to 50 and caps at 200.
   * @format int32
   */
  changeLimit?: number;
}

export interface FeedMappingServiceGetFeedMappingRunParams {
  runId: string;
}

export interface FeedMappingServiceGetSoftFeedMappingRunParams {
  runId: string;
}

export interface FeedMappingServiceListFeedMappingRunEventsParams {
  runId: string;
}

export interface FeedMappingServicePlanFeedMappingRunParams {
  plan: string;
}

export interface FeedMappingServiceRequestWiringVerificationParams {
  verify: string;
}

export interface FirmwareServiceAbortFirmwareArtifactUploadParams {
  uploadId: string;
}

export interface FirmwareServiceCancelFirmwareUpdateParams {
  /**
   * server_id optionally selects which Frigate node owns task_id. Empty means
   * the connected node, preserving pre-cluster behavior.
   */
  serverId?: string;
  taskId: string;
}

export interface FirmwareServiceCommitFirmwareArtifactUploadParams {
  uploadId: string;
}

export interface FirmwareServiceGetFirmwareUpdateParams {
  /**
   * server_id optionally selects which Frigate node owns task_id. Empty means
   * the connected node, preserving pre-cluster behavior.
   */
  serverId?: string;
  taskId: string;
}

export interface FirmwareServiceListFirmwareUpdatesParams {
  /**
   * local_only is used by HA peers to avoid recursive fanout when aggregating
   * cluster update lists. Ordinary clients leave it false.
   */
  localOnly?: boolean;
}

export interface FirmwareServiceWriteFirmwareArtifactUploadChunkBody {
  /** @format byte */
  data?: string;
  /**
   * image_index selects an entry in the begin request's metadata.images.
   * @format int64
   */
  imageIndex?: number;
  /**
   * offset is the byte offset of data within the selected image.
   * @format int64
   */
  offset?: string;
}

export interface FirmwareServiceWriteFirmwareArtifactUploadChunkParams {
  uploadId: string;
}

export interface HashboardServiceGetHashboardAsiCsParams {
  /** identifier accepts a CT1 MAC address or R2 serial number. */
  identifier: string;
}

export interface HashboardServiceGetHashboardAsicMetadataParams {
  /** identifier accepts a CT1 MAC address or R2 serial number. */
  identifier: string;
}

export interface HashboardServiceGetHashboardEventsParams {
  /** @format date-time */
  "window.end"?: string;
  /** @format date-time */
  "window.start"?: string;
  /** identifier accepts a CT1 MAC address or R2 serial number. */
  identifier: string;
}

export interface HashboardServiceGetHashboardParams {
  /** identifier accepts a CT1 MAC address or R2 serial number. */
  identifier: string;
}

export interface HashboardServiceGetRecoveryEvidenceParams {
  evidenceId: string;
}

export interface HashboardServiceListRecoveryEvidenceParams {
  /** episode_id optionally selects snapshots for one recovery episode. */
  episodeId?: string;
  /** identifier optionally selects one CT1 hashboard by MAC address. */
  identifier?: string;
  /**
   * limit bounds the result; the server defaults and caps it.
   * @format int64
   */
  limit?: number;
}

export interface MQTTSourceConnectionConfigBroker {
  host?: string;
  /** @format int64 */
  port?: number;
}

export type ManufacturingServiceAdoptManufacturingBoardBody = object;

export interface ManufacturingServiceAdoptManufacturingBoardParams {
  identifier: string;
}

export interface ManufacturingServiceGetManufacturingSelfTestParams {
  testId: string;
}

export interface ManufacturingServiceListManufacturingSelfTestsParams {
  identifier?: string;
  /** @format int32 */
  limit?: number;
}

/** Tag represents a key-value metadata tag. */
export interface MdkV2Tag {
  key?: string;
  value?: string;
}

export interface MiningGroupServiceDeleteMiningGroupParams {
  /** @format int64 */
  expectedRevision?: string;
  id: string;
  idempotencyKey?: string;
}

export interface MiningGroupServiceGetMiningGroupOperationParams {
  id: string;
}

export interface MiningGroupServiceGetMiningGroupParams {
  id: string;
}

export interface MiningGroupServiceGetMiningGroupPoolDiagnosticsParams {
  groupId: string;
}

export interface MiningGroupServiceGetMiningGroupTelemetryParams {
  groupId: string;
  /** Empty selects all current members. */
  macAddresses?: string[];
}

export interface MiningGroupServiceListMiningGroupMembershipsParams {
  groupId?: string;
}

export interface MiningGroupServiceListMiningGroupPoolsParams {
  groupId: string;
}

export interface MiningGroupServiceQueryMiningGroupHistoryParams {
  /** @format int64 */
  bucketSeconds?: number;
  /** @format date-time */
  end?: string;
  groupId: string;
  /** Empty selects members at observation time. */
  macAddresses?: string[];
  /** @format date-time */
  start?: string;
}

export interface MiningGroupServiceReplaceMiningGroupPoolsBody {
  /** @format int64 */
  expectedRevision?: string;
  idempotencyKey?: string;
  pools?: V2MiningGroupPoolInput[];
}

export interface MiningGroupServiceReplaceMiningGroupPoolsParams {
  groupId: string;
}

export interface MiningGroupServiceTestMiningGroupPoolConnectionParams {
  groupId: string;
  idempotencyKey?: string;
  /** @format int32 */
  poolId: number;
}

export interface MiningGroupServiceUpdateMiningGroupBody {
  /** @format int64 */
  expectedRevision?: string;
  idempotencyKey?: string;
  /** Writable customer metadata only; operating policy uses the intent RPC. */
  metadata?: V2MiningGroupMetadata;
  /**
   * Required and nonempty; must contain name and/or external_ref.
   * Paths are relative to metadata. Unselected fields are preserved;
   * selected absent external_ref clears it. Metadata never changes intent.
   */
  updateMask?: string;
}

export interface MiningGroupServiceUpdateMiningGroupIntentBody {
  desiredMining?: boolean;
  /** @format int64 */
  expectedRevision?: string;
  idempotencyKey?: string;
  pllAlgorithm?: string;
  /** @format double */
  requestedBoardWatts?: number;
  resetRequested?: boolean;
  /**
   * Required and nonempty. Allowed paths: desired_mining,
   * requested_board_watts, pll_algorithm, reset_requested. Unselected values
   * are preserved. Selected absent watts restores entitlement; selected empty
   * PLL restores NONE. The selected tunnel peer must advertise this method.
   */
  updateMask?: string;
}

export interface MiningGroupServiceUpdateMiningGroupIntentParams {
  id: string;
}

export interface MiningGroupServiceUpdateMiningGroupParams {
  id: string;
}

export interface MiningServiceClearModuleFixedVoltageParams {
  moduleId: string;
}

export interface MiningServiceSetModuleFixedVoltageBody {
  /** @format int64 */
  millivolts?: number;
}

export interface MiningServiceSetModuleFixedVoltageParams {
  moduleId: string;
}

export interface PSUFirmwareServiceWritePSUFirmwareCapsuleUploadChunkBody {
  /** @format byte */
  data?: string;
  /**
   * offset is the byte offset of data within the capsule.
   * @format int64
   */
  offset?: string;
}

export interface PoolServiceDeletePoolParams {
  /** @format int64 */
  id: string;
}

export interface PoolServiceGetPoolDiagnosticsParams {
  /**
   * Opt in to a bounded share-log lookup. The default false preserves the
   * database cost of existing high-frequency diagnostics consumers.
   */
  includeLastRecordedShares?: boolean;
}

export interface PoolServiceGetPoolParams {
  /** @format int64 */
  id: string;
}

export interface PoolServiceTestPoolConnectionParams {
  /** @format int64 */
  id: string;
}

export interface PoolServiceUpdatePoolBody {
  /**
   * password is the pool authentication password (write-only, never returned in
   * responses). An EMPTY password preserves the stored credential; there is
   * currently no way to clear a stored password via this API.
   */
  password?: string;
  /**
   * Pool represents a mining pool configuration.
   * The password field is write-only and never included in responses.
   */
  pool?: {
    /** @format date-time */
    createdAt?: string;
    enabled?: boolean;
    name?: string;
    /** @format int32 */
    priority?: number;
    /**
     * share_weight enables weighted mining for this pool. It is a relative,
     * positive weight; when absent the pool remains a priority failover target.
     * @format double
     */
    shareWeight?: number;
    url?: string;
    username?: string;
  };
  /**
   * update_mask selects which Pool fields to apply; unselected fields are
   * backfilled from the stored record so a partial write (for example an
   * enabled-only toggle) can never erase configuration. Valid paths: name,
   * url, username, priority, enabled, share_weight (plus id, a no-op);
   * unknown paths are refused. A masked share_weight is
   * presence-authoritative: an absent value clears the stored weight. A pool
   * name can never be cleared; an empty masked name preserves the stored one.
   *
   * Compatibility: an EMPTY mask keeps the legacy contract — all Pool fields
   * are applied, and an absent share_weight preserves the stored weight. The
   * historical single-path {"share_weight"} mask likewise applies the full
   * record while treating share_weight absence as a clear, because deployed
   * TUI clients send it that way with every update.
   */
  updateMask?: string;
}

export interface PoolServiceUpdatePoolParams {
  /** @format int64 */
  poolId: string;
}

/**
 * ClearPowerFeedOverrideRequest clears one power feed's process-local operator
 * marker.
 */
export type PowerServiceClearPowerFeedOverrideBody = object;

export interface PowerServiceClearPowerFeedOverrideParams {
  /**
   * power_feed_id is the container power-feed identifier, e.g.
   * "pdu-1-outlet-03"; bound from the path.
   */
  powerFeedId: string;
}

export type PowerServiceCyclePowerFeedBody = object;

export interface PowerServiceCyclePowerFeedParams {
  cycle: string;
  /** power_feed_id is the enabled, non-faulted container power-feed identifier. */
  powerFeedId: string;
}

/** SetContainerControlRequest switches one boolean container control. */
export interface PowerServiceSetContainerControlBody {
  /** enabled selects the target state: true starts/enables, false stops/disables. */
  enabled?: boolean;
  /**
   * Deprecated compatibility bit for Frigate versions that enforced
   * application-side plant safety policy. Current servers ignore it.
   */
  force?: boolean;
}

export interface PowerServiceSetContainerControlParams {
  /** name is the control identifier; bound from the path. */
  name: string;
}

/** SetContainerSetpointRequest writes one analog container setpoint. */
export interface PowerServiceSetContainerSetpointBody {
  /**
   * Deprecated compatibility bit for Frigate versions that enforced
   * application-side plant safety policy. Current servers ignore it.
   */
  force?: boolean;
  /**
   * value must be representable as a finite float32 PLC wire value. The PLC
   * owns operational limits and final command acceptance.
   * @format double
   */
  value?: number;
}

export interface PowerServiceSetContainerSetpointParams {
  /** name is the setpoint identifier; bound from the path. */
  name: string;
}

export interface PowerServiceSetPowerFeedStateBody {
  disabled?: boolean;
  /**
   * Deprecated compatibility bit for Frigate versions that enforced
   * application-side plant safety policy. Current servers ignore it.
   */
  force?: boolean;
}

export interface PowerServiceSetPowerFeedStateParams {
  powerFeedId: string;
}

/**
 * `Any` contains an arbitrary serialized protocol buffer message along with a
 * URL that describes the type of the serialized message.
 *
 * Protobuf library provides support to pack/unpack Any values in the form
 * of utility functions or additional generated methods of the Any type.
 *
 * Example 1: Pack and unpack a message in C++.
 *
 *     Foo foo = ...;
 *     Any any;
 *     any.PackFrom(foo);
 *     ...
 *     if (any.UnpackTo(&foo)) {
 *       ...
 *     }
 *
 * Example 2: Pack and unpack a message in Java.
 *
 *     Foo foo = ...;
 *     Any any = Any.pack(foo);
 *     ...
 *     if (any.is(Foo.class)) {
 *       foo = any.unpack(Foo.class);
 *     }
 *     // or ...
 *     if (any.isSameTypeAs(Foo.getDefaultInstance())) {
 *       foo = any.unpack(Foo.getDefaultInstance());
 *     }
 *
 *  Example 3: Pack and unpack a message in Python.
 *
 *     foo = Foo(...)
 *     any = Any()
 *     any.Pack(foo)
 *     ...
 *     if any.Is(Foo.DESCRIPTOR):
 *       any.Unpack(foo)
 *       ...
 *
 *  Example 4: Pack and unpack a message in Go
 *
 *      foo := &pb.Foo{...}
 *      any, err := anypb.New(foo)
 *      if err != nil {
 *        ...
 *      }
 *      ...
 *      foo := &pb.Foo{}
 *      if err := any.UnmarshalTo(foo); err != nil {
 *        ...
 *      }
 *
 * The pack methods provided by protobuf library will by default use
 * 'type.googleapis.com/full.type.name' as the type URL and the unpack
 * methods only use the fully qualified type name after the last '/'
 * in the type URL, for example "foo.bar.com/x/y.z" will yield type
 * name "y.z".
 *
 * JSON
 * ====
 * The JSON representation of an `Any` value uses the regular
 * representation of the deserialized, embedded message, with an
 * additional field `@type` which contains the type URL. Example:
 *
 *     package google.profile;
 *     message Person {
 *       string first_name = 1;
 *       string last_name = 2;
 *     }
 *
 *     {
 *       "@type": "type.googleapis.com/google.profile.Person",
 *       "firstName": <string>,
 *       "lastName": <string>
 *     }
 *
 * If the embedded message type is well-known and has a custom JSON
 * representation, that representation will be embedded adding a field
 * `value` which holds the custom JSON in addition to the `@type`
 * field. Example (for message [google.protobuf.Duration][]):
 *
 *     {
 *       "@type": "type.googleapis.com/google.protobuf.Duration",
 *       "value": "1.212s"
 *     }
 */
export interface ProtobufAny {
  /**
   * A URL/resource name that uniquely identifies the type of the serialized
   * protocol buffer message. This string must contain at least
   * one "/" character. The last segment of the URL's path must represent
   * the fully qualified name of the type (as in
   * `path/google.protobuf.Duration`). The name should be in a canonical form
   * (e.g., leading "." is not accepted).
   *
   * In practice, teams usually precompile into the binary all types that they
   * expect it to use in the context of Any. However, for URLs which use the
   * scheme `http`, `https`, or no scheme, one can optionally set up a type
   * server that maps type URLs to message definitions as follows:
   *
   * * If no scheme is provided, `https` is assumed.
   * * An HTTP GET on the URL must yield a [google.protobuf.Type][]
   *   value in binary format, or produce an error.
   * * Applications are allowed to cache lookup results based on the
   *   URL, or have them precompiled into a binary to avoid any
   *   lookup. Therefore, binary compatibility needs to be preserved
   *   on changes to types. (Use versioned type names to manage
   *   breaking changes.)
   *
   * Note: this functionality is not currently available in the official
   * protobuf release, and it is not used for type URLs beginning with
   * type.googleapis.com. As of May 2023, there are no widely used type server
   * implementations and no plans to implement one.
   *
   * Schemes other than `http`, `https` (or the empty scheme) might be
   * used with implementation specific semantics.
   */
  "@type"?: string;
  [key: string]: any;
}

export interface PsuFirmwareServiceAbortPsuFirmwareCapsuleUploadParams {
  uploadId: string;
}

export interface PsuFirmwareServiceCancelPsuFirmwareUpdateParams {
  taskId: string;
}

export interface PsuFirmwareServiceCommitPsuFirmwareCapsuleUploadParams {
  uploadId: string;
}

export interface PsuFirmwareServiceGetPsuFirmwareBoardStatusParams {
  macAddress: string;
}

export interface PsuFirmwareServiceGetPsuFirmwareUpdateParams {
  taskId: string;
}

export interface PsuFirmwareServiceWritePsuFirmwareCapsuleUploadChunkParams {
  uploadId: string;
}

/**
 * The `Status` type defines a logical error model that is suitable for
 * different programming environments, including REST APIs and RPC APIs. It is
 * used by [gRPC](https://github.com/grpc). Each `Status` message contains
 * three pieces of data: error code, error message, and error details.
 *
 * You can find out more about this error model and how to work with it in the
 * [API Design Guide](https://cloud.google.com/apis/design/errors).
 */
export interface RpcStatus {
  /**
   * The status code, which should be an enum value of
   * [google.rpc.Code][google.rpc.Code].
   * @format int32
   */
  code?: number;
  /**
   * A list of messages that carry the error details.  There is a common set of
   * message types for APIs to use.
   */
  details?: ProtobufAny[];
  /**
   * A developer-facing error message, which should be in English. Any
   * user-facing error message should be localized and sent in the
   * [google.rpc.Status.details][google.rpc.Status.details] field, or localized
   * by the client.
   */
  message?: string;
}

export interface RunInServiceCancelHashboardRunInParams {
  /** Blank preserves the legacy local-only lookup during rolling deployment. */
  serverId?: string;
  taskId: string;
}

export interface RunInServiceDownloadHashboardRunInResultsParams {
  taskId: string;
}

export interface RunInServiceGetHashboardRunInStatusParams {
  /** Blank preserves the legacy local-only lookup during rolling deployment. */
  serverId?: string;
  taskId: string;
}

export interface RunInServiceListHashboardRunInTasksParams {
  /** local_only prevents peer fanout loops on peer-internal reads. */
  localOnly?: boolean;
}

export interface RunInServiceStartHashboardRunInParams {
  identifier: string;
  /** task_id is an optional caller-generated idempotency key for this task. */
  taskId?: string;
}

export interface RunInServiceStartModuleRunInParams {
  moduleId: string;
  /** task_id is an optional caller-generated idempotency key for this task. */
  taskId?: string;
}

export interface SystemServiceGetLogsParams {
  component?: string;
  /** @default "LOG_LEVEL_UNSPECIFIED" */
  level?:
    | "LOG_LEVEL_UNSPECIFIED"
    | "LOG_LEVEL_DEBUG"
    | "LOG_LEVEL_INFO"
    | "LOG_LEVEL_WARNING"
    | "LOG_LEVEL_ERROR";
  /** @format int32 */
  tail?: number;
}

export interface SystemServiceListAuditEventsParams {
  /** @format date-time */
  "range.end"?: string;
  /** @format date-time */
  "range.start"?: string;
  /** Exact-match filters; empty means no filter. */
  action?: string;
  actor?: string;
  /**
   * Maximum entries to return; 0 applies the server default (200), capped
   * at 1000.
   * @format int32
   */
  limit?: number;
  result?: string;
  source?: string;
  target?: string;
}

export interface SystemServiceUpdatePsuFirmwareParams {
  psuId: string;
}

export interface SystemServiceWatchFirmwareUpdateParams {
  operationId: string;
}

export interface TelemetryServiceGetContainerHealthParams {
  /**
   * Opt in to the shared-database module summary. Default callers do not add
   * location or hashboard-health reads to the plant snapshot.
   */
  includeModuleDistribution?: boolean;
}

export interface TelemetryServiceGetHashboardLifetimeParams {
  identifier: string;
}

export interface TelemetryServiceGetShareQualityParams {
  /**
   * identifier is an optional CT1 MAC address or R2 serial number that
   * restricts only the by_board breakdown. It does not filter by
   * pool and is not a pool name.
   */
  identifier?: string;
  /** @default "SHARE_QUALITY_WINDOW_UNSPECIFIED" */
  window?:
    | "SHARE_QUALITY_WINDOW_UNSPECIFIED"
    | "SHARE_QUALITY_WINDOW_15M"
    | "SHARE_QUALITY_WINDOW_1H"
    | "SHARE_QUALITY_WINDOW_24H";
}

export interface TelemetryServiceListContainerPlantAlarmEpisodesParams {
  /** @format date-time */
  "range.end"?: string;
  /** @format date-time */
  "range.start"?: string;
  active?: boolean;
  componentId?: string;
  /** @format int64 */
  limit?: number;
  pageToken?: string;
  severities?: (
    | "SEVERITY_UNSPECIFIED"
    | "SEVERITY_INFO"
    | "SEVERITY_WARNING"
    | "SEVERITY_ERROR"
    | "SEVERITY_CRITICAL"
  )[];
}

export interface TelemetryServiceListContainerPlantEventsParams {
  /** @format date-time */
  "range.end"?: string;
  /** @format date-time */
  "range.start"?: string;
  category?: string;
  componentId?: string;
  /** @format int64 */
  limit?: number;
}

export interface TelemetryServiceQueryProductionHistoryParams {
  /** @format date-time */
  "range.end"?: string;
  /** @format date-time */
  "range.start"?: string;
  /** @default "BUCKET_SIZE_UNSPECIFIED" */
  bucketSize?:
    | "BUCKET_SIZE_UNSPECIFIED"
    | "BUCKET_SIZE_1M"
    | "BUCKET_SIZE_5M"
    | "BUCKET_SIZE_15M"
    | "BUCKET_SIZE_1H"
    | "BUCKET_SIZE_1D";
  identifier?: string;
}

export interface TelemetryServiceQueryShareQualityHistoryParams {
  /** @format date-time */
  "range.end"?: string;
  /** @format date-time */
  "range.start"?: string;
  /** @default "BUCKET_SIZE_UNSPECIFIED" */
  bucketSize?:
    | "BUCKET_SIZE_UNSPECIFIED"
    | "BUCKET_SIZE_1M"
    | "BUCKET_SIZE_5M"
    | "BUCKET_SIZE_15M"
    | "BUCKET_SIZE_1H"
    | "BUCKET_SIZE_1D";
  /** @default "SHARE_QUALITY_HISTORY_GROUPING_UNSPECIFIED" */
  grouping?:
    | "SHARE_QUALITY_HISTORY_GROUPING_UNSPECIFIED"
    | "SHARE_QUALITY_HISTORY_GROUPING_TOTAL"
    | "SHARE_QUALITY_HISTORY_GROUPING_BOARD"
    | "SHARE_QUALITY_HISTORY_GROUPING_POOL";
  identifier?: string;
  /** @format int32 */
  poolId?: number;
}

export interface TelemetryServiceQueryTankProductionHistoryParams {
  /** @format int64 */
  bucketMinutes?: number;
  /** @format date-time */
  from?: string;
  tankKey: string;
  /** @format date-time */
  to?: string;
}

export interface TelemetryServiceQueryTankSensorHistoryParams {
  /** @format date-time */
  from?: string;
  /** @format int64 */
  limit?: number;
  tankKey: string;
  /** @format date-time */
  to?: string;
}

export interface TelemetryServiceQueryTimeSeriesParams {
  /** @format date-time */
  "range.end"?: string;
  /** @format date-time */
  "range.start"?: string;
  /** @format int64 */
  "temperatureSelector.boardSensorIndex"?: number;
  /** @default "PSU_TEMPERATURE_SENSOR_UNSPECIFIED" */
  "temperatureSelector.psuSensor"?:
    | "PSU_TEMPERATURE_SENSOR_UNSPECIFIED"
    | "PSU_TEMPERATURE_SENSOR_INLET"
    | "PSU_TEMPERATURE_SENSOR_SR_HEATSINK"
    | "PSU_TEMPERATURE_SENSOR_PFC_HEATSINK";
  /** @default "BUCKET_SIZE_UNSPECIFIED" */
  bucketSize?:
    | "BUCKET_SIZE_UNSPECIFIED"
    | "BUCKET_SIZE_1M"
    | "BUCKET_SIZE_5M"
    | "BUCKET_SIZE_15M"
    | "BUCKET_SIZE_1H"
    | "BUCKET_SIZE_1D";
  /**
   * identifier is a CT1 MAC address or R2 serial number for board metrics. It
   * is the phase, distribution-unit, fan, or power-feed ID for component plant
   * metrics.
   */
  identifier?: string;
  /**
   *  - TIME_SERIES_METRIC_PLANT_INLET_TEMPERATURE: Plant metrics are container-scoped and ignore the identifier field.
   *  - TIME_SERIES_METRIC_PLANT_PHASE_POWER: Component metrics require identifier to select a phase, distribution
   * unit, fan, or power feed.
   *  - TIME_SERIES_METRIC_TEMPERATURE_SENSOR: Sensor-specific board or PSU temperature history. Requests must include a
   * TemperatureSeriesSelector; the legacy TEMPERATURE metric remains the
   * aggregate across every board sensor.
   * @default "TIME_SERIES_METRIC_UNSPECIFIED"
   */
  metric?:
    | "TIME_SERIES_METRIC_UNSPECIFIED"
    | "TIME_SERIES_METRIC_HASHRATE"
    | "TIME_SERIES_METRIC_POWER"
    | "TIME_SERIES_METRIC_TEMPERATURE"
    | "TIME_SERIES_METRIC_VOLTAGE"
    | "TIME_SERIES_METRIC_PLANT_INLET_TEMPERATURE"
    | "TIME_SERIES_METRIC_PLANT_OUTLET_TEMPERATURE"
    | "TIME_SERIES_METRIC_PLANT_POWER"
    | "TIME_SERIES_METRIC_PLANT_AMBIENT_TEMPERATURE"
    | "TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_POWER"
    | "TIME_SERIES_METRIC_PLANT_FACILITY_POWER"
    | "TIME_SERIES_METRIC_PLANT_REACTIVE_POWER"
    | "TIME_SERIES_METRIC_PLANT_APPARENT_POWER"
    | "TIME_SERIES_METRIC_PLANT_ACTIVE_ENERGY"
    | "TIME_SERIES_METRIC_PLANT_REACTIVE_ENERGY"
    | "TIME_SERIES_METRIC_PLANT_APPARENT_ENERGY"
    | "TIME_SERIES_METRIC_PLANT_POWER_FACTOR"
    | "TIME_SERIES_METRIC_PLANT_PUE"
    | "TIME_SERIES_METRIC_PLANT_DRY_COOLER_INLET_TEMPERATURE"
    | "TIME_SERIES_METRIC_PLANT_OUTLET_A_TEMPERATURE"
    | "TIME_SERIES_METRIC_PLANT_OUTLET_B_TEMPERATURE"
    | "TIME_SERIES_METRIC_PLANT_PUMP_OUTLET_PRESSURE"
    | "TIME_SERIES_METRIC_PLANT_PUMP_INLET_PRESSURE"
    | "TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_INLET_PRESSURE"
    | "TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_OUTLET_PRESSURE"
    | "TIME_SERIES_METRIC_PLANT_TANK_A_LIQUID_LEVEL"
    | "TIME_SERIES_METRIC_PLANT_TANK_B_LIQUID_LEVEL"
    | "TIME_SERIES_METRIC_PLANT_FLOW_RATE"
    | "TIME_SERIES_METRIC_PLANT_AMBIENT_HUMIDITY"
    | "TIME_SERIES_METRIC_PLANT_FILTER_PRESSURE_DIFFERENTIAL"
    | "TIME_SERIES_METRIC_PLANT_PUMP_PRESSURE_DIFFERENTIAL"
    | "TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_PRESSURE_DIFFERENTIAL"
    | "TIME_SERIES_METRIC_PLANT_COOLANT_PUMP_SPEED"
    | "TIME_SERIES_METRIC_PLANT_PHASE_POWER"
    | "TIME_SERIES_METRIC_PLANT_PHASE_CURRENT"
    | "TIME_SERIES_METRIC_PLANT_PHASE_VOLTAGE"
    | "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_ACTIVE_FEEDS"
    | "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_VOLTAGE_AB"
    | "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_VOLTAGE_BC"
    | "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_VOLTAGE_CA"
    | "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_CURRENT_A"
    | "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_CURRENT_B"
    | "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_CURRENT_C"
    | "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_POWER"
    | "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_ACTIVE_ENERGY"
    | "TIME_SERIES_METRIC_PLANT_FAN_POWER"
    | "TIME_SERIES_METRIC_PLANT_FAN_SPEED"
    | "TIME_SERIES_METRIC_POWER_FEED_POWER"
    | "TIME_SERIES_METRIC_POWER_FEED_CURRENT"
    | "TIME_SERIES_METRIC_POWER_FEED_VOLTAGE"
    | "TIME_SERIES_METRIC_POWER_FEED_VOLTAGE_AB"
    | "TIME_SERIES_METRIC_POWER_FEED_VOLTAGE_BC"
    | "TIME_SERIES_METRIC_POWER_FEED_VOLTAGE_CA"
    | "TIME_SERIES_METRIC_POWER_FEED_CURRENT_A"
    | "TIME_SERIES_METRIC_POWER_FEED_CURRENT_B"
    | "TIME_SERIES_METRIC_POWER_FEED_CURRENT_C"
    | "TIME_SERIES_METRIC_TEMPERATURE_SENSOR"
    | "TIME_SERIES_METRIC_PLANT_INSIDE_AMBIENT_TEMPERATURE"
    | "TIME_SERIES_METRIC_PLANT_INSIDE_AMBIENT_HUMIDITY"
    | "TIME_SERIES_METRIC_PLANT_OUTSIDE_AMBIENT_TEMPERATURE"
    | "TIME_SERIES_METRIC_PLANT_OUTSIDE_AMBIENT_HUMIDITY";
}

export interface TelemetryServiceStreamAsicHealthParams {
  /** identifier accepts a CT1 MAC address or R2 serial number. */
  identifier: string;
}

export interface TelemetryServiceStreamHashboardHealthParams {
  /** identifier accepts a CT1 MAC address or R2 serial number. */
  identifier?: string;
}

/** ASIC represents per-ASIC health data on a hashboard. */
export interface V2ASIC {
  /**
   * hashrate_ghs10 is the hashrate in GH/s * 10 (e.g. 125 = 12.5 GH/s).
   * @format int32
   */
  hashrateGhs10?: number;
  /**
   * compact_index is the original zero-based CT1 firmware identity. It is
   * absent when the producer cannot map this sample to a physical position.
   * @format int32
   */
  compactIndex?: number;
  /**
   * debug_health is the newest sampled production debug-health observation
   * for this ASIC from the shared database, which may have been recorded by
   * either HA node. It is absent when the Frigate node serving this request
   * has opt-in production debug-health capture disabled (even if its HA peer
   * captures the board), when firmware does not publish debug-health frames,
   * when no sample is newer than three sample intervals, or when the newest
   * sample reported a different ASIC count than this health frame. It can lag
   * the health frame by up to one sample interval plus a short read cache;
   * compare observed_at with the frame time before treating both as current.
   */
  debugHealth?: V2ASICDebugHealth;
  /**
   * frequency_mhz is the ASIC clock frequency in megahertz.
   * @format int32
   */
  frequencyMhz?: number;
  /**
   * index is the zero-based physical ASIC position on the hashboard.
   * @format int32
   */
  index?: number;
  /**
   * pass_rate_bp is the nonce pass rate in basis points (e.g. 9950 = 99.50%).
   * @format int32
   */
  passRateBp?: number;
  /**
   * temperature_dc is the ASIC temperature in decicelsius (e.g. 725 = 72.5 C).
   * @format int32
   */
  temperatureDc?: number;
  /**
   * ThermalQuarantined is present when firmware supplied a valid thermal-
   * throttle snapshot covering this ASIC. Absence means not reported.
   */
  thermalQuarantined?: boolean;
  /**
   * voltage_mv is the ASIC voltage in millivolts.
   * @format int32
   */
  voltageMv?: number;
}

/**
 * ASICDebugHealth is one sampled firmware debug-health observation for one
 * ASIC: ECC counters and raw voltage-domain sensor readings.
 */
export interface V2ASICDebugHealth {
  /**
   * Raw MC3 voltage-domain sensor readings in volts, as reported by firmware.
   * @format double
   */
  vddi1V?: number;
  /** @format double */
  vddi2V?: number;
  /** @format double */
  vddi3V?: number;
  /**
   * ecc_counter_reset reports that a counter or the firmware slot counter went
   * backwards since the baseline, for example after an undetected firmware
   * restart or a slot counter wrap. No delta is reported for that sample.
   */
  eccCounterReset?: boolean;
  /**
   * ecc_delta counts errors since ecc_delta_since. It is absent for the first
   * sample of a board connection or mining session, after a gap longer than
   * three sample intervals, after the reported ASIC count changed, and when
   * ecc_counter_reset is set.
   */
  eccDelta?: V2ASICECCCounters;
  /**
   * ecc_delta_since is the observation time of the baseline sample that
   * ecc_delta is measured from. It is present exactly when ecc_delta is.
   * @format date-time
   */
  eccDeltaSince?: string;
  /** ecc_total holds the cumulative firmware counters at observed_at. */
  eccTotal?: V2ASICECCCounters;
  /**
   * observed_at is when Frigate accepted the sampled firmware frame.
   * @format date-time
   */
  observedAt?: string;
  /** @format double */
  vddV?: number;
  /** @format double */
  vddhV?: number;
  /** @format double */
  vddioV?: number;
  /** @format double */
  vssV?: number;
}

/**
 * ASICECCCounters holds the three cumulative MC3 ULink ECC (UECC) counters
 * that firmware reports for one ASIC. Zero is also what firmware reports when
 * it does not populate a counter, so zeros do not prove an error-free link.
 */
export interface V2ASICECCCounters {
  /** @format int64 */
  uecc0?: number;
  /** @format int64 */
  uecc1?: number;
  /** @format int64 */
  uecc2?: number;
}

/** ASICLayoutEntry maps an ASIC index to its physical hashboard position. */
export interface V2ASICLayoutEntry {
  /** @format int32 */
  index?: number;
  /** @format int32 */
  stackId?: number;
  /** @format int32 */
  stackIndex?: number;
  /** @format int32 */
  x?: number;
  /** @format int32 */
  y?: number;
}

/**
 * ASICMetadata is decoded, immutable eFuse identity and binning data for one
 * ASIC. index uses the same zero-based physical index as ASIC and
 * HashboardASICLayout.
 */
export interface V2ASICMetadata {
  /** @format int64 */
  binning?: number;
  /** @format int64 */
  binningVersion?: number;
  /** @format int64 */
  dieX?: number;
  /** @format int64 */
  dieY?: number;
  /**
   * grwvpl is the ASIC manufacturer part number.
   * @format uint64
   */
  grwvpl?: string;
  /** @format int32 */
  index?: number;
  lotId?: string;
  /** valid is true when firmware successfully decoded this ASIC's eFuse data. */
  valid?: boolean;
  waferId?: string;
}

/** ASICStopCriteria applies to ASIC-level telemetry. */
export interface V2ASICStopCriteria {
  /** @format int32 */
  expectedAsicCount?: number;
  /** @format int32 */
  maxFailingAsics?: number;
  /** @format double */
  maxTemperature?: number;
  /** @format double */
  minHashrate?: number;
  /** @format double */
  minPassRate?: number;
}

export type V2AbortFirmwareArtifactUploadResponse = object;

export type V2AbortPSUFirmwareCapsuleUploadResponse = object;

export interface V2AcknowledgeAlertResponse {
  /** Alert represents a system alert. */
  alert?: V2Alert;
}

export interface V2ActivateMiningGroupsRequest {
  /** @format int64 */
  expectedStateRevision?: string;
  idempotencyKey?: string;
}

export interface V2ActivateMiningGroupsResponse {
  capability?: V2MiningGroupCapability;
}

export interface V2AdoptManufacturingBoardResponse {
  /** @format date-time */
  adoptedAt?: string;
  adoptedBy?: string;
  macAddress?: string;
}

/** Alert represents a system alert. */
export interface V2Alert {
  acknowledged?: boolean;
  /** @format date-time */
  acknowledgedAt?: string;
  category?: string;
  code?: string;
  /** @format date-time */
  createdAt?: string;
  details?: string;
  hashboardMac?: string;
  /** @format int64 */
  id?: string;
  message?: string;
  /**
   * resource_id is the source's stable ID for non-hashboard sources. R2
   * hashboards use serial instead.
   */
  resourceId?: string;
  /** serial is the stable R2 hashboard identity and is empty for other sources. */
  serial?: string;
  /** Severity represents the severity level of an alert. */
  severity?: V2Severity;
  /**
   * slot is one-based when the source has a physical chassis slot.
   * @format int64
   */
  slot?: number;
  /** AlertSourceKind identifies the typed resource class that emitted an alert. */
  sourceKind?: V2AlertSourceKind;
}

/**
 * AlertSourceKind identifies the typed resource class that emitted an alert.
 * @default "ALERT_SOURCE_KIND_UNSPECIFIED"
 */
export enum V2AlertSourceKind {
  ALERT_SOURCE_KIND_UNSPECIFIED = "ALERT_SOURCE_KIND_UNSPECIFIED",
  ALERT_SOURCE_KIND_SYSTEM = "ALERT_SOURCE_KIND_SYSTEM",
  ALERT_SOURCE_KIND_FAN = "ALERT_SOURCE_KIND_FAN",
  ALERT_SOURCE_KIND_CONTROL_BOARD = "ALERT_SOURCE_KIND_CONTROL_BOARD",
  ALERT_SOURCE_KIND_POWER_SUPPLY = "ALERT_SOURCE_KIND_POWER_SUPPLY",
  ALERT_SOURCE_KIND_HASHBOARD = "ALERT_SOURCE_KIND_HASHBOARD",
}

export interface V2ApplyFirmwareUpdateRequest {
  /** Nondefault options with an already-current version return FAILED_PRECONDITION. */
  options?: V2SystemUpdateOptions;
  /** Exact version returned by CheckFirmwareUpdate. */
  version?: string;
}

export interface V2ApplyFirmwareUpdateResponse {
  accepted?: boolean;
  message?: string;
  /**
   * Durable update ID until history is expired or cleared; empty for an
   * already-current no-op or legacy response.
   */
  operationId?: string;
}

export interface V2ApplyManagedOperationalPolicyRequest {
  fingerprint?: string;
  policy?: V2ManagedOperationalPolicy;
  /** @format uint64 */
  profileRevision?: string;
}

export interface V2ApplyManagedOperationalPolicyResponse {
  state?: V2OperationalPolicyState;
}

export interface V2ApplySoftFeedMappingRunResponse {
  /** SoftFeedMappingRun is a snapshot of a soft feed mapping run. */
  run?: V2SoftFeedMappingRun;
}

/** AuditEvent is one recorded operator- or system-initiated action. */
export interface V2AuditEvent {
  /** Action is a dotted verb, e.g. "mining.stop". */
  action?: string;
  /** Actor is the client-supplied identity ("user@host") or subsystem name. */
  actor?: string;
  /** Structured JSON payload; empty when the action recorded no details. */
  detailsJson?: string;
  error?: string;
  /** @format int64 */
  id?: string;
  /** Node is the server ID of the node that performed the action. */
  node?: string;
  /** Result is "ok" or "error". */
  result?: string;
  /** Source is "grpc:<addr>", "rest:<addr>", or "system". */
  source?: string;
  target?: string;
  /** @format date-time */
  time?: string;
}

/**
 * AuthStatus contains local authentication posture rather than operational
 * system health. Presence distinguishes an authoritative false value from an
 * implementation that cannot report the field.
 */
export interface V2AuthStatus {
  onboarded?: boolean;
  passwordSet?: boolean;
}

export interface V2BeginFirmwareArtifactUploadRequest {
  metadata?: V2FirmwareUploadMetadata;
}

export interface V2BeginFirmwareArtifactUploadResponse {
  /**
   * expires_at is when the upload is discarded unless another call uses it.
   * @format date-time
   */
  expiresAt?: string;
  /**
   * max_chunk_bytes is the largest accepted chunk data size.
   * @format int64
   */
  maxChunkBytes?: number;
  /** upload_id identifies the upload on the connected node only. */
  uploadId?: string;
}

export interface V2BeginPSUFirmwareCapsuleUploadRequest {
  metadata?: V2PSUFirmwareCapsuleUploadMetadata;
}

export interface V2BeginPSUFirmwareCapsuleUploadResponse {
  /**
   * expires_at is when the upload is discarded unless another call uses it.
   * @format date-time
   */
  expiresAt?: string;
  /**
   * max_chunk_bytes is the largest accepted chunk data size.
   * @format int64
   */
  maxChunkBytes?: number;
  /** upload_id identifies the upload on the connected node only. */
  uploadId?: string;
}

/** BoardResult reports the per-board outcome of a scoped operation. */
export interface V2BoardResult {
  /** True if the operation reached this board. */
  applied?: boolean;
  macAddress?: string;
  /** Reason when applied is false (not-managed/reserved/disconnected). */
  message?: string;
}

/**
 * BoardScope selects hashboards. identifiers are MAC addresses or serials; modules
 * selects whole container modules by tank/module. The selections union (OR); at
 * least one is required.
 */
export interface V2BoardScope {
  identifiers?: string[];
  modules?: V2ModuleSelector[];
}

/** BoardTemperatureSensor is one board-level temperature sample with its physical identity. */
export interface V2BoardTemperatureSensor {
  /**
   * index is the zero-based firmware sensor index.
   * @format int32
   */
  index?: number;
  /** BoardTemperatureSensorLocation identifies where a board temperature sensor is mounted. */
  location?: V2BoardTemperatureSensorLocation;
  /**
   * temperature is the board temperature in degrees Celsius.
   * @format double
   */
  temperature?: number;
}

/**
 * BoardTemperatureSensorLocation identifies where a board temperature sensor is mounted.
 * @default "BOARD_TEMPERATURE_SENSOR_LOCATION_UNSPECIFIED"
 */
export enum V2BoardTemperatureSensorLocation {
  BOARD_TEMPERATURE_SENSOR_LOCATION_UNSPECIFIED = "BOARD_TEMPERATURE_SENSOR_LOCATION_UNSPECIFIED",
  BOARD_TEMPERATURE_SENSOR_LOCATION_POWER = "BOARD_TEMPERATURE_SENSOR_LOCATION_POWER",
  BOARD_TEMPERATURE_SENSOR_LOCATION_FLOW_IN = "BOARD_TEMPERATURE_SENSOR_LOCATION_FLOW_IN",
  BOARD_TEMPERATURE_SENSOR_LOCATION_FLOW_OUT = "BOARD_TEMPERATURE_SENSOR_LOCATION_FLOW_OUT",
  BOARD_TEMPERATURE_SENSOR_LOCATION_MCU = "BOARD_TEMPERATURE_SENSOR_LOCATION_MCU",
  BOARD_TEMPERATURE_SENSOR_LOCATION_ASIC_FRONT = "BOARD_TEMPERATURE_SENSOR_LOCATION_ASIC_FRONT",
  BOARD_TEMPERATURE_SENSOR_LOCATION_ASIC_REAR = "BOARD_TEMPERATURE_SENSOR_LOCATION_ASIC_REAR",
  BOARD_TEMPERATURE_SENSOR_LOCATION_PCBA = "BOARD_TEMPERATURE_SENSOR_LOCATION_PCBA",
  BOARD_TEMPERATURE_SENSOR_LOCATION_ASIC_CENTERLINE = "BOARD_TEMPERATURE_SENSOR_LOCATION_ASIC_CENTERLINE",
  BOARD_TEMPERATURE_SENSOR_LOCATION_ASIC_CORNER = "BOARD_TEMPERATURE_SENSOR_LOCATION_ASIC_CORNER",
}

export interface V2CancelFeedMappingRunResponse {
  /** FeedMappingRun is a snapshot of a feed mapping run. */
  run?: V2FeedMappingRun;
}

export interface V2CancelFirmwareUpdateResponse {
  /** FirmwareUpdate is one firmware update job. */
  update?: V2FirmwareUpdate;
}

export interface V2CancelHashboardRunInResponse {
  /** HashboardRunInTask is a status snapshot for a run-in task. */
  task?: V2HashboardRunInTask;
}

export interface V2CancelPSUFirmwareUpdateResponse {
  /** PSUFirmwareUpdate is one module update job. */
  update?: V2PSUFirmwareUpdate;
}

export interface V2CancelSoftFeedMappingRunResponse {
  /** SoftFeedMappingRun is a snapshot of a soft feed mapping run. */
  run?: V2SoftFeedMappingRun;
}

export interface V2ChangePasswordRequest {
  currentPassword?: string;
  newPassword?: string;
}

export type V2ChangePasswordResponse = object;

/** CheckFirmwareUpdateResponse contains firmware update availability details. */
export interface V2CheckFirmwareUpdateResponse {
  available?: boolean;
  releaseNotesUrl?: string;
  /** @format int64 */
  sizeBytes?: string;
  version?: string;
}

export interface V2ClearModuleFixedVoltageResponse {
  /** @format int32 */
  boardsUpdated?: number;
  results?: V2BoardResult[];
  /**
   * ModuleVoltageSetting is one durable module voltage setting. An absent
   * fixed_millivolts field means firmware automatic voltage tuning.
   */
  setting?: V2ModuleVoltageSetting;
}

/**
 * ClearPowerFeedOverrideResponse returns the unchanged power-feed state with
 * its operator marker cleared.
 */
export interface V2ClearPowerFeedOverrideResponse {
  /** PowerFeed contains one container power feed and optional telemetry. */
  powerFeed?: V2PowerFeed;
}

export interface V2ClearPowerTargetScopedRequest {
  /**
   * BoardScope selects hashboards. identifiers are MAC addresses or serials; modules
   * selects whole container modules by tank/module. The selections union (OR); at
   * least one is required.
   */
  scope?: V2BoardScope;
}

export interface V2ClearPowerTargetScopedResponse {
  /** @format int32 */
  boardsUpdated?: number;
  results?: V2BoardResult[];
}

export interface V2CommitFirmwareArtifactUploadResponse {
  /** FirmwareArtifactRef identifies one deployable firmware artifact. */
  artifact?: V2FirmwareArtifactRef;
  images?: V2FirmwareImage[];
}

export interface V2CommitPSUFirmwareCapsuleUploadResponse {
  /** PSUFirmwareCapsule describes one locally verified, cached capsule. */
  capsule?: V2PSUFirmwareCapsule;
}

/** ContainerControl is one boolean tank/container plant control. */
export interface V2ContainerControl {
  /**
   * Deprecated application-side safety classification. Current servers always
   * return false because the PLC owns plant safety interlocks.
   */
  coolingCritical?: boolean;
  description?: string;
  /**
   * enabled is the control's current (or just-written) state. Only meaningful
   * when state_known is true.
   */
  enabled?: boolean;
  /**
   * name is the backend control identifier, e.g. "coolant_pump" or
   * "tank_led_light".
   */
  name?: string;
  /**
   * state_known is true when enabled reflects a state read back from the
   * controller rather than an unknown default.
   */
  stateKnown?: boolean;
}

/**
 * ContainerControlConfirmation records one fresh register read after a write.
 * Cached or echoed desired values never establish confirmation.
 */
export interface V2ContainerControlConfirmation {
  /** @format date-time */
  observedAt?: string;
  observedEnabled?: boolean;
  requestedEnabled?: boolean;
  source?: V2PlantWriteConfirmationSource;
  /**  - PLANT_WRITE_CONFIRMATION_STATE_UNAVAILABLE: The write succeeded, but authoritative observation is unavailable. */
  state?: V2PlantWriteConfirmationState;
}

/** ContainerMap represents the full physical layout of a CT1 container. */
export interface V2ContainerMap {
  tanks?: V2Tank[];
}

export interface V2ContainerOperatingParameter {
  boundary?: string;
  code?: string;
  unit?: string;
  /** @format double */
  value?: number;
}

export interface V2ContainerPlantAlarmEpisode {
  active?: boolean;
  alarmId?: string;
  /** @format date-time */
  clearedAt?: string;
  componentId?: string;
  firstSeenTruncated?: boolean;
  name?: string;
  /** @format int64 */
  openingEventId?: string;
  /** Severity represents the severity level of an alert. */
  severity?: V2Severity;
  /** @format date-time */
  startedAt?: string;
}

/** ContainerPlantEvent is one durable discrete state transition. */
export interface V2ContainerPlantEvent {
  affectedComponentId?: string;
  boolValue?: boolean;
  category?: string;
  componentId?: string;
  /** @format int64 */
  id?: string;
  name?: string;
  /** @format double */
  numberValue?: number;
  /** @format date-time */
  observedAt?: string;
  previousBoolValue?: boolean;
  /** @format double */
  previousNumberValue?: number;
  previousTextValue?: string;
  /** Severity represents the severity level of an alert. */
  severity?: V2Severity;
  textValue?: string;
  unit?: string;
}

/** ContainerSetpoint is one writable analog tank/container setting. */
export interface V2ContainerSetpoint {
  /**
   * Legacy presence marker. True means min and max contain documented range
   * metadata; it does not imply application-side enforcement.
   */
  boundsKnown?: boolean;
  description?: string;
  /** @format double */
  max?: number;
  /**
   * Documented PLC range metadata for operator display. Frigate does not use
   * these values as an application-side write limit.
   * @format double
   */
  min?: number;
  /**
   * name is the backend setting identifier, e.g. "coolant_pump_speed_hz" or
   * "dry_cooler_auto_temp_c".
   */
  name?: string;
  unit?: string;
  /**
   * value is the current (or just-applied) setpoint value.
   * @format double
   */
  value?: number;
}

/**
 * ContainerSetpointConfirmation compares readback with the float32 wire value,
 * retaining the original request separately so rounding is explicit.
 */
export interface V2ContainerSetpointConfirmation {
  /** @format date-time */
  observedAt?: string;
  /** @format double */
  observedValue?: number;
  /** @format double */
  requestedValue?: number;
  source?: V2PlantWriteConfirmationSource;
  /**  - PLANT_WRITE_CONFIRMATION_STATE_UNAVAILABLE: The write succeeded, but authoritative observation is unavailable. */
  state?: V2PlantWriteConfirmationState;
  /** @format double */
  wireValue?: number;
}

/** ControlBoardInfo contains stable control-board hardware identity. */
export interface V2ControlBoardInfo {
  boardModel?: string;
  hardwareRevision?: string;
  machineName?: string;
  manufacturer?: string;
  serialNumber?: string;
  socModel?: string;
}

/** CoolingConfig contains cooling system configuration. */
export interface V2CoolingConfig {
  /**
   * FanMode represents the fan control strategy.
   *
   *  - FAN_MODE_OFF: OFF explicitly disables fans when platform cooling policy permits it.
   */
  fanMode?: V2FanMode;
  /** @format int32 */
  fanSpeedPercent?: number;
  /** @format double */
  targetTemperature?: number;
}

/** CoolingFanInfo contains one installed fan's immutable operating range. */
export interface V2CoolingFanInfo {
  /**
   * fan_id is a required, nonzero device-local identity. It is unique within
   * HardwareInfo.cooling_fans and joins this inventory entry to
   * CoolingFanStatus.fan_id.
   * @format int64
   */
  fanId: number;
  /** @format int64 */
  maximumRpm?: number;
  /** @format int64 */
  minimumRpm?: number;
  name?: string;
  /**
   * slot is one-based when the platform reports a physical fan position.
   * @format int64
   */
  slot?: number;
}

/** CoolingFanStatus contains one fan's observed operating state. */
export interface V2CoolingFanStatus {
  /**
   * fan_id is a required, nonzero device-local identity. It is unique within
   * GetCoolingConfigResponse.fans and must match a CoolingFanInfo.fan_id
   * returned by GetHardwareInfo.
   * @format int64
   */
  fanId: number;
  /** @format int64 */
  rpm?: number;
  /**
   * slot is one-based when the platform reports a physical fan position.
   * @format int64
   */
  slot?: number;
  /** @format int64 */
  speedPercent?: number;
}

export interface V2CreateMiningGroupRequest {
  idempotencyKey?: string;
  /** Writable customer metadata only; operating policy uses the intent RPC. */
  metadata?: V2MiningGroupMetadata;
}

export interface V2CreateMiningGroupResponse {
  group?: V2MiningGroupRecord;
}

export interface V2CreatePoolRequest {
  /** password is the pool authentication password (write-only, never returned in responses). */
  password?: string;
  /**
   * Pool represents a mining pool configuration.
   * The password field is write-only and never included in responses.
   */
  pool?: V2Pool;
}

export interface V2CreatePoolResponse {
  /**
   * Pool represents a mining pool configuration.
   * The password field is write-only and never included in responses.
   */
  pool?: V2Pool;
}

export interface V2CyclePowerFeedResponse {
  /** PowerFeed contains one container power feed and optional telemetry. */
  powerFeed?: V2PowerFeed;
}

export interface V2DeactivateMiningGroupsRequest {
  /** @format int64 */
  expectedStateRevision?: string;
  idempotencyKey?: string;
}

export interface V2DeactivateMiningGroupsResponse {
  capability?: V2MiningGroupCapability;
}

export type V2DeleteMiningGroupResponse = object;

export type V2DeletePoolResponse = object;

export interface V2DescribeReadOnlyDatabaseResponse {
  tables?: V2ReadOnlyDatabaseTable[];
}

/** @default "DIAGNOSTICS_DATABASE_ROLE_UNSPECIFIED" */
export enum V2DiagnosticsDatabaseRole {
  DIAGNOSTICS_DATABASE_ROLE_UNSPECIFIED = "DIAGNOSTICS_DATABASE_ROLE_UNSPECIFIED",
  DIAGNOSTICS_DATABASE_ROLE_PRIMARY = "DIAGNOSTICS_DATABASE_ROLE_PRIMARY",
  DIAGNOSTICS_DATABASE_ROLE_STANDBY = "DIAGNOSTICS_DATABASE_ROLE_STANDBY",
}

/**
 * DiagnosticsDatabaseRouting identifies whether a relation can be read from a
 * physical standby without losing non-WAL-replicated data.
 * @default "DIAGNOSTICS_DATABASE_ROUTING_UNSPECIFIED"
 */
export enum V2DiagnosticsDatabaseRouting {
  DIAGNOSTICS_DATABASE_ROUTING_UNSPECIFIED = "DIAGNOSTICS_DATABASE_ROUTING_UNSPECIFIED",
  DIAGNOSTICS_DATABASE_ROUTING_PRIMARY_ONLY = "DIAGNOSTICS_DATABASE_ROUTING_PRIMARY_ONLY",
  DIAGNOSTICS_DATABASE_ROUTING_REPLICA_SAFE = "DIAGNOSTICS_DATABASE_ROUTING_REPLICA_SAFE",
}

export interface V2DiagnosticsDatabaseTarget {
  instance?: string;
  /** @format int64 */
  lagBytes?: string;
  /**
   * primary_fallback is true when a replica-safe query ran on the primary only
   * because no healthy standby was available and the container opted in to the
   * tighter-capped primary fallback.
   */
  primaryFallback?: boolean;
  role?: V2DiagnosticsDatabaseRole;
  /**
   * DiagnosticsDatabaseRouting identifies whether a relation can be read from a
   * physical standby without losing non-WAL-replicated data.
   */
  routing?: V2DiagnosticsDatabaseRouting;
}

/** @default "DRY_COOLER_DIRECTION_MODE_UNSPECIFIED" */
export enum V2DryCoolerDirectionMode {
  DRY_COOLER_DIRECTION_MODE_UNSPECIFIED = "DRY_COOLER_DIRECTION_MODE_UNSPECIFIED",
  DRY_COOLER_DIRECTION_MODE_FORWARD = "DRY_COOLER_DIRECTION_MODE_FORWARD",
  DRY_COOLER_DIRECTION_MODE_REVERSE = "DRY_COOLER_DIRECTION_MODE_REVERSE",
  DRY_COOLER_DIRECTION_MODE_UNKNOWN = "DRY_COOLER_DIRECTION_MODE_UNKNOWN",
}

/** @default "DRY_COOLER_FAN_REVERSE_STATE_UNSPECIFIED" */
export enum V2DryCoolerFanReverseState {
  DRY_COOLER_FAN_REVERSE_STATE_UNSPECIFIED = "DRY_COOLER_FAN_REVERSE_STATE_UNSPECIFIED",
  DRY_COOLER_FAN_REVERSE_STATE_STOPPED = "DRY_COOLER_FAN_REVERSE_STATE_STOPPED",
  DRY_COOLER_FAN_REVERSE_STATE_STARTING = "DRY_COOLER_FAN_REVERSE_STATE_STARTING",
  DRY_COOLER_FAN_REVERSE_STATE_IN_PROGRESS = "DRY_COOLER_FAN_REVERSE_STATE_IN_PROGRESS",
  DRY_COOLER_FAN_REVERSE_STATE_FINISHED = "DRY_COOLER_FAN_REVERSE_STATE_FINISHED",
  DRY_COOLER_FAN_REVERSE_STATE_UNKNOWN = "DRY_COOLER_FAN_REVERSE_STATE_UNKNOWN",
}

export interface V2DryCoolerFanReverseStatus {
  /** @format int64 */
  durationMinutes?: number;
  state?: V2DryCoolerFanReverseState;
}

export interface V2EffectiveOperationalPolicy {
  /** @format double */
  acDisabledLoadThresholdKw?: number;
  /** @format double */
  insideAmbientCriticalC?: number;
  /** @format double */
  insideAmbientHysteresisC?: number;
  /** @format double */
  insideAmbientWarningC?: number;
  /** @format double */
  maxBoardWatts?: number;
  /** @format double */
  minBoardWatts?: number;
  /** Legacy numeric values retained for mixed-version Flotilla deployments. */
  recoveryCauses?: number[];
  recoveryEnabled?: boolean;
  recoveryErrorCauses?: V2HashboardErrorCause[];
  recoveryHoldoff?: string;
  recoveryOffDuration?: string;
  /** Recovery scope is intentionally fixed and reported for operator clarity. */
  recoveryScope?: string;
  /** @format double */
  safetyMargin?: number;
}

export interface V2EnableVoltageTuningResponse {
  /** @format int32 */
  boardsUpdated?: number;
}

/**
 * FanMode represents the fan control strategy.
 *
 *  - FAN_MODE_OFF: OFF explicitly disables fans when platform cooling policy permits it.
 * @default "FAN_MODE_UNSPECIFIED"
 */
export enum V2FanMode {
  FAN_MODE_UNSPECIFIED = "FAN_MODE_UNSPECIFIED",
  FAN_MODE_AUTO = "FAN_MODE_AUTO",
  FAN_MODE_MANUAL = "FAN_MODE_MANUAL",
  FAN_MODE_OFF = "FAN_MODE_OFF",
}

/** FeedMappingChange is one durable transition in the current feed map. */
export interface V2FeedMappingChange {
  /** @format date-time */
  changedAt?: string;
  feedId?: string;
  macAddress?: string;
  previousFeedId?: string;
  runId?: string;
  /** @format int64 */
  sequence?: string;
  serial?: string;
  /** FeedMappingChangeType classifies a durable current-map transition. */
  type?: V2FeedMappingChangeType;
}

/**
 * FeedMappingChangeType classifies a durable current-map transition.
 * @default "FEED_MAPPING_CHANGE_TYPE_UNSPECIFIED"
 */
export enum V2FeedMappingChangeType {
  FEED_MAPPING_CHANGE_TYPE_UNSPECIFIED = "FEED_MAPPING_CHANGE_TYPE_UNSPECIFIED",
  FEED_MAPPING_CHANGE_TYPE_ADDED = "FEED_MAPPING_CHANGE_TYPE_ADDED",
  FEED_MAPPING_CHANGE_TYPE_REMOVED = "FEED_MAPPING_CHANGE_TYPE_REMOVED",
  FEED_MAPPING_CHANGE_TYPE_MOVED = "FEED_MAPPING_CHANGE_TYPE_MOVED",
}

/** FeedMappingDetectedBoard identifies a board detected during feed mapping. */
export interface V2FeedMappingDetectedBoard {
  detectedBy?: string;
  firmwareVersion?: string;
  /** @format date-time */
  firstSeenAt?: string;
  hwRevision?: string;
  /** @format date-time */
  lastSeenAt?: string;
  macAddress?: string;
  previousFeedId?: string;
  serial?: string;
  /** @format date-time */
  verifiedAt?: string;
}

/** FeedMappingFeedResult contains mapping results for one feed. */
export interface V2FeedMappingFeedResult {
  boards?: V2FeedMappingDetectedBoard[];
  /** @format date-time */
  completedAt?: string;
  feedId?: string;
  offlineTimedOut?: boolean;
  /** @format date-time */
  startedAt?: string;
  /** FeedMappingFeedStatus is the lifecycle state for one feed in a run. */
  status?: V2FeedMappingFeedStatus;
  timedOut?: boolean;
}

/**
 * FeedMappingFeedStatus is the lifecycle state for one feed in a run.
 * @default "FEED_MAPPING_FEED_STATUS_UNSPECIFIED"
 */
export enum V2FeedMappingFeedStatus {
  FEED_MAPPING_FEED_STATUS_UNSPECIFIED = "FEED_MAPPING_FEED_STATUS_UNSPECIFIED",
  FEED_MAPPING_FEED_STATUS_PENDING = "FEED_MAPPING_FEED_STATUS_PENDING",
  FEED_MAPPING_FEED_STATUS_RUNNING = "FEED_MAPPING_FEED_STATUS_RUNNING",
  FEED_MAPPING_FEED_STATUS_COMPLETED = "FEED_MAPPING_FEED_STATUS_COMPLETED",
  FEED_MAPPING_FEED_STATUS_TIMED_OUT = "FEED_MAPPING_FEED_STATUS_TIMED_OUT",
}

/** FeedMappingRun is a snapshot of a feed mapping run. */
export interface V2FeedMappingRun {
  /** @format date-time */
  completedAt?: string;
  currentFeedId?: string;
  dryRun?: boolean;
  error?: string;
  feeds?: V2FeedMappingFeedResult[];
  phase?: string;
  restoreAllOn?: boolean;
  runId?: string;
  /** @format date-time */
  startedAt?: string;
  /** FeedMappingRunStatus is the lifecycle state of a feed mapping run. */
  status?: V2FeedMappingRunStatus;
  /** @format date-time */
  updatedAt?: string;
  warnings?: string[];
}

/** FeedMappingRunConfig configures feed mapping run behavior. */
export interface V2FeedMappingRunConfig {
  /** Required for mutating runs. */
  confirmPowerCycle?: boolean;
  /** Validate and return a plan without writing hardware state. */
  dryRun?: boolean;
  /**
   * Optional early-completion count for each feed.
   * @format int32
   */
  expectedBoardsPerFeed?: number;
  /** Feed IDs to map. Empty means all configured power feeds for the active backend. */
  feedIds?: string[];
  /** Maximum time to wait for boards after enabling one feed. */
  feedTimeout?: string;
  /** Quiet window after disabling all feeds before mapping starts. */
  isolationQuiet?: string;
  /** Quiet window used to confirm mapped boards went offline after a feed is disabled. */
  offlineQuiet?: string;
  /** Grace period after the expected boards appear before accepting a feed. */
  onlineSettle?: string;
  /** Restore all isolation feeds to ON when the run completes. */
  restoreAllOn?: boolean;
}

/** FeedMappingRunEvent is an event emitted during feed mapping runs. */
export interface V2FeedMappingRunEvent {
  feedId?: string;
  macAddress?: string;
  message?: string;
  phase?: string;
  /** @format int64 */
  seq?: string;
  severity?: string;
  /** @format date-time */
  time?: string;
  type?: string;
}

/**
 * FeedMappingRunStatus is the lifecycle state of a feed mapping run.
 * @default "FEED_MAPPING_RUN_STATUS_UNSPECIFIED"
 */
export enum V2FeedMappingRunStatus {
  FEED_MAPPING_RUN_STATUS_UNSPECIFIED = "FEED_MAPPING_RUN_STATUS_UNSPECIFIED",
  FEED_MAPPING_RUN_STATUS_PLANNED = "FEED_MAPPING_RUN_STATUS_PLANNED",
  FEED_MAPPING_RUN_STATUS_RUNNING = "FEED_MAPPING_RUN_STATUS_RUNNING",
  FEED_MAPPING_RUN_STATUS_COMPLETED = "FEED_MAPPING_RUN_STATUS_COMPLETED",
  FEED_MAPPING_RUN_STATUS_FAILED = "FEED_MAPPING_RUN_STATUS_FAILED",
  FEED_MAPPING_RUN_STATUS_CANCELED = "FEED_MAPPING_RUN_STATUS_CANCELED",
}

/** FirmwareArtifactRef identifies one deployable firmware artifact. */
export interface V2FirmwareArtifactRef {
  /** @format int64 */
  artifactId?: string;
  artifactName?: string;
  /** @format int64 */
  assetId?: string;
  assetName?: string;
  /** Branch fields (source = BRANCH). */
  branch?: string;
  headSha?: string;
  /**
   * Pull-request fields (source = PR). artifact_id and artifact_name are
   * shared with BRANCH refs.
   * @format int32
   */
  prNumber?: number;
  /** Release fields (source = RELEASE). */
  releaseTag?: string;
  /**
   * FirmwareArtifactSource identifies where a firmware artifact comes from.
   *
   *  - FIRMWARE_ARTIFACT_SOURCE_UPLOAD: UPLOAD identifies content delivered by an operator client and cached by
   * its content digest. RELEASE, PR, and BRANCH remain for source
   * attribution/history.
   *  - FIRMWARE_ARTIFACT_SOURCE_BRANCH: BRANCH identifies a workflow artifact built by a push to a branch
   * (typically tip of main).
   */
  source?: V2FirmwareArtifactSource;
  /** upload_id is the server-assigned content digest for an uploaded bundle. */
  uploadId?: string;
}

/**
 * FirmwareArtifactSource identifies where a firmware artifact comes from.
 *
 *  - FIRMWARE_ARTIFACT_SOURCE_UPLOAD: UPLOAD identifies content delivered by an operator client and cached by
 * its content digest. RELEASE, PR, and BRANCH remain for source
 * attribution/history.
 *  - FIRMWARE_ARTIFACT_SOURCE_BRANCH: BRANCH identifies a workflow artifact built by a push to a branch
 * (typically tip of main).
 * @default "FIRMWARE_ARTIFACT_SOURCE_UNSPECIFIED"
 */
export enum V2FirmwareArtifactSource {
  FIRMWARE_ARTIFACT_SOURCE_UNSPECIFIED = "FIRMWARE_ARTIFACT_SOURCE_UNSPECIFIED",
  FIRMWARE_ARTIFACT_SOURCE_RELEASE = "FIRMWARE_ARTIFACT_SOURCE_RELEASE",
  FIRMWARE_ARTIFACT_SOURCE_PR = "FIRMWARE_ARTIFACT_SOURCE_PR",
  FIRMWARE_ARTIFACT_SOURCE_UPLOAD = "FIRMWARE_ARTIFACT_SOURCE_UPLOAD",
  FIRMWARE_ARTIFACT_SOURCE_BRANCH = "FIRMWARE_ARTIFACT_SOURCE_BRANCH",
}

/**
 * FirmwareImage is the decoded metadata of one firmware .bin inside an
 * artifact.
 */
export interface V2FirmwareImage {
  /** sha256 is the digest of the complete signed image file. */
  sha256?: string;
  build?: string;
  /** @format date-time */
  builtAt?: string;
  /**
   * compatible_hardware_revisions lists the board hardware revisions this
   * image can flash, resolved server-side from hardware_revision with the
   * same hbmeta family/metadata gates used by deployment preflight. Empty
   * means the server did not resolve compatibility (older server or unknown
   * revision string), not universal compatibility.
   */
  compatibleHardwareRevisions?: V2HardwareRev[];
  gitBranch?: string;
  gitId?: string;
  hardwareRevision?: string;
  /** hash is the exact embedded firmware identity reported after boot. */
  hash?: string;
  name?: string;
  version?: string;
}

export interface V2FirmwarePolicy {
  /** FirmwareArtifactRef identifies one deployable firmware artifact. */
  artifact?: V2FirmwareArtifactRef;
  artifactReady?: boolean;
  build?: string;
  enabled?: boolean;
  failureMode?: V2FirmwarePolicyFailureMode;
  /** @format uint64 */
  generation?: string;
  images?: V2FirmwareImage[];
  /** @format int64 */
  maxParallel?: number;
  /** @format date-time */
  updatedAt?: string;
  updatedBy?: string;
}

export interface V2FirmwarePolicyBoard {
  /** @format int64 */
  attempts?: number;
  macAddress?: string;
  message?: string;
  /** @format date-time */
  nextRetryAt?: string;
  /** @format date-time */
  observedAt?: string;
  observedBuild?: string;
  observedGitId?: string;
  observedHash?: string;
  observedVersion?: string;
  /** @format uint64 */
  policyGeneration?: string;
  reason?: string;
  status?: V2FirmwarePolicyBoardStatus;
  taskId?: string;
  /** @format date-time */
  updatedAt?: string;
}

/** @default "FIRMWARE_POLICY_BOARD_STATUS_UNSPECIFIED" */
export enum V2FirmwarePolicyBoardStatus {
  FIRMWARE_POLICY_BOARD_STATUS_UNSPECIFIED = "FIRMWARE_POLICY_BOARD_STATUS_UNSPECIFIED",
  FIRMWARE_POLICY_BOARD_STATUS_DISABLED = "FIRMWARE_POLICY_BOARD_STATUS_DISABLED",
  FIRMWARE_POLICY_BOARD_STATUS_PENDING_IDENTITY = "FIRMWARE_POLICY_BOARD_STATUS_PENDING_IDENTITY",
  FIRMWARE_POLICY_BOARD_STATUS_COMPLIANT = "FIRMWARE_POLICY_BOARD_STATUS_COMPLIANT",
  FIRMWARE_POLICY_BOARD_STATUS_QUEUED = "FIRMWARE_POLICY_BOARD_STATUS_QUEUED",
  FIRMWARE_POLICY_BOARD_STATUS_UPDATING = "FIRMWARE_POLICY_BOARD_STATUS_UPDATING",
  FIRMWARE_POLICY_BOARD_STATUS_DEFERRED = "FIRMWARE_POLICY_BOARD_STATUS_DEFERRED",
  FIRMWARE_POLICY_BOARD_STATUS_FAILED_HELD = "FIRMWARE_POLICY_BOARD_STATUS_FAILED_HELD",
  FIRMWARE_POLICY_BOARD_STATUS_FAILED_ADMITTED = "FIRMWARE_POLICY_BOARD_STATUS_FAILED_ADMITTED",
  FIRMWARE_POLICY_BOARD_STATUS_EXEMPT_ISOLATED = "FIRMWARE_POLICY_BOARD_STATUS_EXEMPT_ISOLATED",
  FIRMWARE_POLICY_BOARD_STATUS_EXCLUDED_RUN_IN = "FIRMWARE_POLICY_BOARD_STATUS_EXCLUDED_RUN_IN",
}

/** @default "FIRMWARE_POLICY_FAILURE_MODE_UNSPECIFIED" */
export enum V2FirmwarePolicyFailureMode {
  FIRMWARE_POLICY_FAILURE_MODE_UNSPECIFIED = "FIRMWARE_POLICY_FAILURE_MODE_UNSPECIFIED",
  FIRMWARE_POLICY_FAILURE_MODE_STRICT_HOLD = "FIRMWARE_POLICY_FAILURE_MODE_STRICT_HOLD",
  FIRMWARE_POLICY_FAILURE_MODE_BEST_EFFORT = "FIRMWARE_POLICY_FAILURE_MODE_BEST_EFFORT",
}

/** FirmwareTargetSelector selects exactly one R2 board by its stable serial. */
export interface V2FirmwareTargetSelector {
  serialNumber?: string;
}

/**
 * FirmwareTargetStatus is the lifecycle state of one board within a job.
 * @default "FIRMWARE_TARGET_STATUS_UNSPECIFIED"
 */
export enum V2FirmwareTargetStatus {
  FIRMWARE_TARGET_STATUS_UNSPECIFIED = "FIRMWARE_TARGET_STATUS_UNSPECIFIED",
  FIRMWARE_TARGET_STATUS_QUEUED = "FIRMWARE_TARGET_STATUS_QUEUED",
  FIRMWARE_TARGET_STATUS_STOPPING_MINING = "FIRMWARE_TARGET_STATUS_STOPPING_MINING",
  FIRMWARE_TARGET_STATUS_PREFLIGHT = "FIRMWARE_TARGET_STATUS_PREFLIGHT",
  FIRMWARE_TARGET_STATUS_FLASHING = "FIRMWARE_TARGET_STATUS_FLASHING",
  FIRMWARE_TARGET_STATUS_REBOOTING = "FIRMWARE_TARGET_STATUS_REBOOTING",
  FIRMWARE_TARGET_STATUS_VERIFYING = "FIRMWARE_TARGET_STATUS_VERIFYING",
  FIRMWARE_TARGET_STATUS_RESTORING_MINING = "FIRMWARE_TARGET_STATUS_RESTORING_MINING",
  FIRMWARE_TARGET_STATUS_SUCCEEDED = "FIRMWARE_TARGET_STATUS_SUCCEEDED",
  FIRMWARE_TARGET_STATUS_SKIPPED_UP_TO_DATE = "FIRMWARE_TARGET_STATUS_SKIPPED_UP_TO_DATE",
  FIRMWARE_TARGET_STATUS_FAILED = "FIRMWARE_TARGET_STATUS_FAILED",
}

/** FirmwareUpdate is one firmware update job. */
export interface V2FirmwareUpdate {
  /** FirmwareArtifactRef identifies one deployable firmware artifact. */
  artifact?: V2FirmwareArtifactRef;
  /** @format date-time */
  completedAt?: string;
  error?: string;
  /**
   * FirmwareUpdateOptions tune one update job. Proto3 zero values give the safe
   * defaults: stop mining before flashing and restore it afterwards.
   */
  options?: V2FirmwareUpdateOptions;
  /**
   * server_id is the CT1 Frigate node that owns this update job. It is empty
   * for R2, whose singleton device service already identifies the owner.
   */
  serverId?: string;
  /** @format date-time */
  startedAt?: string;
  /** FirmwareUpdateStatus is the lifecycle state of an update job. */
  status?: V2FirmwareUpdateStatus;
  targets?: V2FirmwareUpdateTarget[];
  taskId?: string;
}

/**
 * FirmwareUpdateOptions tune one update job. Proto3 zero values give the safe
 * defaults: stop mining before flashing and restore it afterwards.
 */
export interface V2FirmwareUpdateOptions {
  /**
   * build restricts image selection to one build type (e.g. "prod", "dev",
   * "mfgtest") when an artifact carries several builds per hardware family.
   * Empty requires the artifact to be unambiguous.
   */
  build?: string;
  /** dry_run stops after preflight and reports what would happen. */
  dryRun?: boolean;
  /** force flashes even when the image version is not newer. */
  force?: boolean;
  /**
   * max_parallel bounds concurrent flashes; 0 uses the server default.
   * @format int64
   */
  maxParallel?: number;
  /** skip_restore_mining leaves boards idle after a successful flash. */
  skipRestoreMining?: boolean;
  /**
   * skip_stop_mining refuses boards that are not already idle instead of
   * stopping them.
   */
  skipStopMining?: boolean;
}

/**
 * FirmwareUpdateStatus is the lifecycle state of an update job.
 * @default "FIRMWARE_UPDATE_STATUS_UNSPECIFIED"
 */
export enum V2FirmwareUpdateStatus {
  FIRMWARE_UPDATE_STATUS_UNSPECIFIED = "FIRMWARE_UPDATE_STATUS_UNSPECIFIED",
  FIRMWARE_UPDATE_STATUS_PENDING = "FIRMWARE_UPDATE_STATUS_PENDING",
  FIRMWARE_UPDATE_STATUS_DOWNLOADING = "FIRMWARE_UPDATE_STATUS_DOWNLOADING",
  FIRMWARE_UPDATE_STATUS_RUNNING = "FIRMWARE_UPDATE_STATUS_RUNNING",
  FIRMWARE_UPDATE_STATUS_COMPLETED = "FIRMWARE_UPDATE_STATUS_COMPLETED",
  FIRMWARE_UPDATE_STATUS_COMPLETED_WITH_ERRORS = "FIRMWARE_UPDATE_STATUS_COMPLETED_WITH_ERRORS",
  FIRMWARE_UPDATE_STATUS_FAILED = "FIRMWARE_UPDATE_STATUS_FAILED",
  FIRMWARE_UPDATE_STATUS_CANCELED = "FIRMWARE_UPDATE_STATUS_CANCELED",
  FIRMWARE_UPDATE_STATUS_INTERRUPTED = "FIRMWARE_UPDATE_STATUS_INTERRUPTED",
}

/** FirmwareUpdateTarget reports per-board update state. */
export interface V2FirmwareUpdateTarget {
  fromVersion?: string;
  /** mac_address is the CT1 board identity and must be empty for R2. */
  macAddress?: string;
  /** message carries free-form failure detail. */
  message?: string;
  /** phase details the DFU stage while flashing (preparing/writing/activating). */
  phase?: string;
  /** @format double */
  progressPercent?: number;
  /** reason is a stable failure classifier (e.g. dfu_write_failed). */
  reason?: string;
  /**
   * serial is the stable R2 board identity. CT1 boards may also report it but
   * retain mac_address as their identity.
   */
  serial?: string;
  /**
   * slot is the optional one-based R2 chassis observation.
   * @format int64
   */
  slot?: number;
  /** FirmwareTargetStatus is the lifecycle state of one board within a job. */
  status?: V2FirmwareTargetStatus;
  toVersion?: string;
  /** @format date-time */
  updatedAt?: string;
}

export interface V2FirmwareUploadImage {
  /** sha256 is the lowercase hexadecimal digest of this image. */
  sha256?: string;
  /** name is a safe single-component .bin filename. */
  name?: string;
  /** @format int64 */
  sizeBytes?: string;
}

export interface V2FirmwareUploadMetadata {
  /** images contains at most 64 signed firmware images. */
  images?: V2FirmwareUploadImage[];
  /**
   * source records where the operator obtained the images. It must be
   * RELEASE, PR, or BRANCH; the server returns an UPLOAD reference after
   * caching.
   */
  source?: V2FirmwareArtifactRef;
}

/** FrequencyRange defines requested ASIC PLL frequency sweep points in MHz. */
export interface V2FrequencyRange {
  /** @format int64 */
  endFrequencyMhz?: number;
  /** @format int64 */
  startFrequencyMhz?: number;
  /** @format int64 */
  stepFrequencyMhz?: number;
}

export interface V2GetActiveFeedMappingRunResponse {
  /** FeedMappingRun is a snapshot of a feed mapping run. */
  run?: V2FeedMappingRun;
}

export interface V2GetActivePowerRecoveryCycleResponse {
  cycle?: V2PowerRecoveryCycle;
}

export interface V2GetActiveSoftFeedMappingRunResponse {
  /** SoftFeedMappingRun is a snapshot of a soft feed mapping run. */
  run?: V2SoftFeedMappingRun;
}

export interface V2GetAuthStatusResponse {
  /**
   * AuthStatus contains local authentication posture rather than operational
   * system health. Presence distinguishes an authoritative false value from an
   * implementation that cannot report the field.
   */
  status?: V2AuthStatus;
}

/** GetContainerControlsResponse lists the container plant controls and setpoints. */
export interface V2GetContainerControlsResponse {
  alarmMuteSupported?: boolean;
  /**
   * Alarm commands are separate capabilities because some controllers expose
   * persistent plant controls without alarm reset or mute endpoints. Optional
   * presence lets newer clients preserve alarm actions against older servers
   * that predate these capability fields.
   */
  alarmResetSupported?: boolean;
  controls?: V2ContainerControl[];
  dryCoolerDirection?: V2DryCoolerDirectionMode;
  dryCoolerFanReverse?: V2DryCoolerFanReverseStatus;
  dryCoolerFanReverseSupported?: boolean;
  mainCircuitBreakerClosed?: boolean;
  /**
   * main_circuit_breaker_supported advertises the dedicated open/close
   * command. main_circuit_breaker_closed is authoritative PLC readback.
   */
  mainCircuitBreakerSupported?: boolean;
  operatingParameters?: V2ContainerOperatingParameter[];
  setpoints?: V2ContainerSetpoint[];
  /**
   * supported is false when the configured container backend has no plant
   * controls (for example, ANEN); controls and setpoints are then empty.
   */
  supported?: boolean;
}

/** GetContainerHealthResponse contains a point-in-time container health snapshot. */
export interface V2GetContainerHealthResponse {
  /**
   * Whether the controller can honor PowerService.MuteContainerAlarm on its
   * active command path, reported with live plant telemetry. Older Frigate
   * releases omit it; consumers then keep assuming mute is supported.
   */
  alarmMuteSupported?: boolean;
  alarms?: V2PlantAlarm[];
  controls?: V2ContainerControl[];
  coolingFans?: V2PlantCoolingFan[];
  /** PlantCoolingFan contains one cooling fan's latest readings. */
  dryCoolerDirection?: V2PlantCoolingFanDirection;
  dryCoolerFanReverse?: V2DryCoolerFanReverseStatus;
  faults?: V2PlantFault[];
  /** Present only when include_module_distribution is requested and supported. */
  moduleDistribution?: V2ModuleDistribution;
  operatingParameters?: V2PlantOperatingParameter[];
  phases?: V2PhaseAggregate[];
  /** PlantHealth contains normalized optional container-plant telemetry. */
  plant?: V2PlantHealth;
  powerDistributions?: V2PlantPowerDistribution[];
  powerFeeds?: V2PowerFeed[];
  setpoints?: V2ContainerSetpoint[];
  /** @format date-time */
  timestamp?: string;
}

export interface V2GetContainerMapResponse {
  /** ContainerMap represents the full physical layout of a CT1 container. */
  containerMap?: V2ContainerMap;
}

export interface V2GetCoolingConfigResponse {
  /** CoolingConfig contains cooling system configuration. */
  config?: V2CoolingConfig;
  fans?: V2CoolingFanStatus[];
}

/** GetDevicePowerResponse contains aggregate device power consumption. */
export interface V2GetDevicePowerResponse {
  /** @format double */
  avgVoltage?: number;
  /** @format int32 */
  boardCount?: number;
  /** @format double */
  efficiency?: number;
  /** @format date-time */
  timestamp?: string;
  /** @format double */
  totalAmps?: number;
  /** @format double */
  totalWatts?: number;
}

export interface V2GetFeedMappingReportResponse {
  changes?: V2FeedMappingChange[];
  /** True when more durable changes exist than were returned. */
  changesTruncated?: boolean;
  /** True when the current report succeeded but durable change history could not be read. */
  changesUnavailable?: boolean;
  /** FeedMappingRun is a snapshot of a feed mapping run. */
  report?: V2FeedMappingRun;
  /** Presence advertises fixed-port wiring support; older agents omit this field. */
  wiring?: V2WiringReport;
}

export interface V2GetFeedMappingRunResponse {
  /** FeedMappingRun is a snapshot of a feed mapping run. */
  run?: V2FeedMappingRun;
}

export interface V2GetFirmwarePolicyResponse {
  policy?: V2FirmwarePolicy;
}

export interface V2GetFirmwareUpdateResponse {
  /** FirmwareUpdate is one firmware update job. */
  update?: V2FirmwareUpdate;
}

export interface V2GetHAStatusResponse {
  /** HAStatus is a bounded diagnostic view, not a command-authority grant. */
  status?: V2HAStatus;
}

export interface V2GetHardwareInfoResponse {
  /** HardwareInfo contains hardware details. */
  hardware?: V2HardwareInfo;
}

export interface V2GetHashboardASICMetadataResponse {
  /** HashboardASICLayout describes the physical ASIC layout for a hashboard. */
  layout?: V2HashboardASICLayout;
  metadata?: V2ASICMetadata[];
}

export interface V2GetHashboardASICsResponse {
  asics?: V2ASIC[];
  /** HashboardASICLayout describes the physical ASIC layout for a hashboard. */
  layout?: V2HashboardASICLayout;
}

export interface V2GetHashboardEventsResponse {
  /**
   * effective_window is the actual [start, end) window the server used after
   * applying the default span and clamping, so clients know the real span
   * covered by the returned events.
   */
  effectiveWindow?: V2TimeRange;
  events?: V2HashboardEvent[];
  /** mac_address is the legacy CT1 identity and is empty for R2. */
  macAddress?: string;
  /** serial is the stable R2 identity. */
  serial?: string;
  /**
   * slot is the optional one-based R2 chassis slot.
   * @format int64
   */
  slot?: number;
}

/**
 * GetHashboardLifetimeResponse contains service-observed lifetime runtime.
 * A nil first_observed_at means the selected board has not yet supplied a
 * persisted health sample.
 */
export interface V2GetHashboardLifetimeResponse {
  /**
   * best_effort_hashing_energy_kwh is the portion reconstructed by the
   * explicit operator repair from pre-cutover rows without HA writer identity.
   * @format double
   */
  bestEffortHashingEnergyKwh?: number;
  /**
   * best_effort_hashing_time_ms is the portion of hashing_time_ms inferred by
   * an explicit operator repair from pre-cutover rows without HA writer
   * identity. The remainder is owner-authoritative.
   * @format uint64
   */
  bestEffortHashingTimeMs?: string;
  /** @format date-time */
  firstObservedAt?: string;
  /**
   * hashing_energy_kwh is board-output energy integrated across the same
   * substantiated positive-hashrate intervals as hashing_time_ms. Presence
   * distinguishes an initialized counter from an older server.
   * @format double
   */
  hashingEnergyKwh?: number;
  /**
   * hashing_energy_observed_since marks the hashing-energy counter epoch.
   * @format date-time
   */
  hashingEnergyObservedSince?: string;
  /**
   * hashing_observed_since marks the corrected counter epoch.
   * @format date-time
   */
  hashingObservedSince?: string;
  /**
   * hashing_time_ms is the corrected time the board produced positive finite
   * hashrate in an operational READY or MINING state. Presence distinguishes
   * it from the legacy counter returned by older servers. It includes run-in
   * and internal work that does not use an external pool.
   * @format uint64
   */
  hashingTimeMs?: string;
  /** mac_address is the legacy CT1 identity and is empty for R2. */
  macAddress?: string;
  /**
   * mining_time_ms is the legacy state-based counter retained for older
   * clients. It can include READY or MINING intervals with zero hashrate.
   * @format uint64
   */
  miningTimeMs?: string;
  /**
   * observed_uptime contains only complete-age windows with substantiated
   * observation time. Telemetry gaps longer than 90 seconds are excluded from
   * both powered and observed time.
   */
  observedUptime?: V2HashboardObservedUptimeWindow[];
  /** @format uint64 */
  poweredOnTimeMs?: string;
  /** serial is the stable R2 identity. */
  serial?: string;
  /**
   * slot is the current optional one-based R2 chassis slot.
   * @format int64
   */
  slot?: number;
}

export interface V2GetHashboardResponse {
  /** Hashboard represents a mining hashboard with its current state and health. */
  hashboard?: V2Hashboard;
}

export interface V2GetHashboardRunInStatusResponse {
  /** HashboardRunInTask is a status snapshot for a run-in task. */
  task?: V2HashboardRunInTask;
}

/** GetLogsResponse contains a single log entry from the stream. */
export interface V2GetLogsResponse {
  component?: string;
  /** LogLevel represents the severity level for log filtering. */
  level?: V2LogLevel;
  message?: string;
  /** @format date-time */
  timestamp?: string;
}

export interface V2GetManagedOperationalPolicyResponse {
  state?: V2OperationalPolicyState;
}

export interface V2GetManufacturingSelfTestResponse {
  test?: V2ManufacturingSelfTest;
}

export interface V2GetMiningGroupCapabilityResponse {
  capability?: V2MiningGroupCapability;
}

export interface V2GetMiningGroupOperationResponse {
  operation?: V2MiningGroupOperationRecord;
}

export interface V2GetMiningGroupPoolDiagnosticsResponse {
  /** @format date-time */
  observedAt?: string;
  observedMacAddresses?: string[];
  observerServerId?: string;
  pools?: V2PoolConnectionDiagnostics[];
  sharing?: V2PoolConnectionSharingDiagnostic[];
  weightedMode?: boolean;
}

export interface V2GetMiningGroupResponse {
  group?: V2MiningGroupRecord;
}

export interface V2GetMiningGroupTelemetryResponse {
  boards?: V2MiningGroupBoardTelemetryRecord[];
  telemetry?: V2MiningGroupTelemetryRecord;
}

/** GetMiningStatusResponse contains aggregate mining status. */
export interface V2GetMiningStatusResponse {
  /** @format int32 */
  activeBoards?: number;
  activePool?: string;
  /** @format double */
  averageAsicTemperatureC?: number;
  /** @format double */
  averageHashboardTemperatureC?: number;
  boardsByState?: Record<string, number>;
  /**
   * Current CT1 persisted board measurements with independent coverage and
   * freshness. Partial values cover reporting boards only; legacy numeric
   * fields are populated only with complete shared-assignment coverage.
   */
  currentMeasurements?: V2MiningCurrentMeasurements;
  /** @format double */
  efficiency?: number;
  /**
   * hashrate retains accepted-share-derived TH/s over the trailing hour.
   * @format double
   */
  hashrate?: number;
  /** @format double */
  hashrateIdeal?: number;
  mining?: boolean;
  /**
   * MiningOperatingState identifies the device-level mining outcome. This is
   * intentionally separate from the per-hashboard MiningState state machine.
   */
  operatingState?: V2MiningOperatingState;
  /**
   * CT1 populates power only with complete shared-assignment board coverage.
   * This is hashboard output watts, not facility or PSU input power. Consumers
   * must use current_measurements to distinguish unavailable from real zero.
   * @format double
   */
  power?: number;
  /** @format double */
  shareAcceptanceRate?: number;
  /** @format int32 */
  totalBoards?: number;
  /** @format int64 */
  uptimeSeconds?: string;
}

export interface V2GetNetworkConfigResponse {
  /** NetworkConfig contains network interface configuration. */
  config?: V2NetworkConfig;
}

export interface V2GetPSUFirmwareBoardStatusResponse {
  /** PSUFirmwareBoardStatus is one board's firmware-reported PSU update state. */
  status?: V2PSUFirmwareBoardStatus;
}

export interface V2GetPSUFirmwareUpdateResponse {
  /** PSUFirmwareUpdate is one module update job. */
  update?: V2PSUFirmwareUpdate;
}

export interface V2GetPoolConnectionSharingStateResponse {
  state?: V2PoolConnectionSharingState;
}

/**
 * GetPoolDiagnosticsResponse contains per-configured-enabled-pool runtime
 * connection diagnostics reflecting this node's live pool connections. Only
 * enabled pools are tracked, so disabled pools are absent.
 */
export interface V2GetPoolDiagnosticsResponse {
  /**
   * active_pool_id is the highest-priority active pool for compatibility;
   * inspect each pool's is_active field when weighted_mode is true.
   * @format int64
   */
  activePoolId?: string;
  /**
   * Explicit strategy vocabulary for clients. The current implementation
   * derives it from enabled share weights; future strategies can be added here.
   */
  allocationMode?: V2PoolAllocationMode;
  /**
   * Populated only when requested. Includes configured disabled pools with a
   * retained result even though they have no live PoolConnectionDiagnostics.
   * Empty means no result within the last 30 days, or an older server that
   * does not support this opt-in field.
   */
  lastRecordedShares?: V2PoolRecordedShareResult[];
  pools?: V2PoolConnectionDiagnostics[];
  weightedMode?: boolean;
}

export interface V2GetPoolResponse {
  /**
   * Pool represents a mining pool configuration.
   * The password field is write-only and never included in responses.
   */
  pool?: V2Pool;
}

export interface V2GetRecoveryEvidenceResponse {
  /**
   * evidence_json is the bounded JSON snapshot. Its sources carry explicit
   * ok, unavailable, or error status so readers tolerate missing data.
   */
  evidenceJson?: string;
  /** RecoveryEvidenceSummary describes one evidence snapshot without its payload. */
  summary?: V2RecoveryEvidenceSummary;
}

export interface V2GetSSHConfigResponse {
  /** SSHConfig contains SSH daemon configuration. */
  config?: V2SSHConfig;
}

export interface V2GetShareQualityResponse {
  byBoard?: V2ShareQualityStats[];
  byPool?: V2ShareQualityStats[];
  /**
   * ShareQualityStats contains share acceptance metrics for a single grouping.
   * key is the CT1 MAC address for by_board entries, the pool name for by_pool
   * entries, and empty for the total aggregate. R2 by_board entries use serial.
   */
  total?: V2ShareQualityStats;
  /** ShareQualityWindow selects the rolling window for share quality statistics. */
  window?: V2ShareQualityWindow;
}

export interface V2GetSoftFeedMappingReportResponse {
  /** SoftFeedMappingRun is a snapshot of a soft feed mapping run. */
  report?: V2SoftFeedMappingRun;
}

export interface V2GetSoftFeedMappingRunResponse {
  /** SoftFeedMappingRun is a snapshot of a soft feed mapping run. */
  run?: V2SoftFeedMappingRun;
}

export interface V2GetSystemInfoResponse {
  /** SystemInfo contains static system information. */
  info?: V2SystemInfo;
}

export interface V2GetSystemStatusResponse {
  /** SystemStatus contains current system status. */
  status?: V2SystemStatus;
}

export interface V2GetTargetPowerResponse {
  balanceBays?: boolean;
  /** @format double */
  defaultWatts?: number;
  hashOnDisconnect?: boolean;
  /** @format double */
  maximumWatts?: number;
  /** @format double */
  minimumWatts?: number;
  /**
   * PerformanceMode selects whether the miner prioritizes maximum hashrate or
   * efficiency within its power target. It is distinct from TuningAlgorithm.
   */
  performanceMode?: V2PerformanceMode;
  /**
   * watts is the current applied target.
   * @format double
   */
  watts?: number;
}

/** GetTuningResponse reports the canonical tuning algorithm currently applied. */
export interface V2GetTuningResponse {
  /**
   * TuningAlgorithm names the canonical mining tuning algorithm shared by all
   * products. Implementations map these semantic values to their native tuning
   * engine without exposing product-specific enum fields on the MDK API.
   */
  mode?: V2TuningAlgorithm;
}

export interface V2GetUnlockStatusResponse {
  /**
   * UnlockStatus contains the device hardware lock state, independent of any
   * effective secure-policy override.
   */
  status?: V2UnlockStatus;
}

/**
 * HAIdentityState describes agreement on a durable logical cluster identity.
 * @default "HA_IDENTITY_STATE_UNSPECIFIED"
 */
export enum V2HAIdentityState {
  HA_IDENTITY_STATE_UNSPECIFIED = "HA_IDENTITY_STATE_UNSPECIFIED",
  HA_IDENTITY_STATE_UNAVAILABLE = "HA_IDENTITY_STATE_UNAVAILABLE",
  HA_IDENTITY_STATE_CONFIRMED = "HA_IDENTITY_STATE_CONFIRMED",
  HA_IDENTITY_STATE_CONFLICTED = "HA_IDENTITY_STATE_CONFLICTED",
}

/** HAMember is one probed HA control-plane host. */
export interface V2HAMember {
  /** Detail is a bounded, human-readable reason for the latest probe failure. */
  detail?: string;
  /**
   * DiskUsedPercent is the root filesystem usage reported by the member's
   * node-exporter. Only the witness host is scraped.
   * @format double
   */
  diskUsedPercent?: number;
  /**
   * HostMetricsObservedAt is the most recent successful node-exporter scrape.
   * @format date-time
   */
  hostMetricsObservedAt?: string;
  /**
   * HostMetricsReachable reports whether the latest node-exporter scrape
   * succeeded. Absent when no scrape is configured for the member.
   */
  hostMetricsReachable?: boolean;
  /**
   * LastSeenAt is the most recent successful etcd probe in this process.
   * @format date-time
   */
  lastSeenAt?: string;
  /** Local reports whether this member runs on the reporting Frigate host. */
  local?: boolean;
  /**
   * Name is the etcd member name, or the configured witness name when etcd
   * membership is unavailable.
   */
  name?: string;
  /**
   * ObservedAt is when the latest probe for this member completed.
   * @format date-time
   */
  observedAt?: string;
  /**
   * Reachable reports whether the member's etcd endpoint answered the latest
   * bounded status probe. It is absent when the probe could not determine
   * reachability, such as when listing etcd members failed.
   */
  reachable?: boolean;
  /**
   * HAMemberRole identifies why a host participates in the HA control plane.
   *
   *  - HA_MEMBER_ROLE_FRIGATE: HA_MEMBER_ROLE_FRIGATE is a Frigate control-server peer.
   *  - HA_MEMBER_ROLE_WITNESS: HA_MEMBER_ROLE_WITNESS is an etcd-only voting witness.
   */
  role?: V2HAMemberRole;
  /**
   * TemperatureC is the hottest thermal zone reported by node-exporter.
   * @format double
   */
  temperatureC?: number;
}

/**
 * HAMemberRole identifies why a host participates in the HA control plane.
 *
 *  - HA_MEMBER_ROLE_FRIGATE: HA_MEMBER_ROLE_FRIGATE is a Frigate control-server peer.
 *  - HA_MEMBER_ROLE_WITNESS: HA_MEMBER_ROLE_WITNESS is an etcd-only voting witness.
 * @default "HA_MEMBER_ROLE_UNSPECIFIED"
 */
export enum V2HAMemberRole {
  HA_MEMBER_ROLE_UNSPECIFIED = "HA_MEMBER_ROLE_UNSPECIFIED",
  HA_MEMBER_ROLE_FRIGATE = "HA_MEMBER_ROLE_FRIGATE",
  HA_MEMBER_ROLE_WITNESS = "HA_MEMBER_ROLE_WITNESS",
}

/** HAMemberStatus is one configured member and independently aged observations. */
export interface V2HAMemberStatus {
  containerWritePrimary?: boolean;
  etcdName?: string;
  /** HAObservation preserves source time independently of response and transport time. */
  etcdObservation?: V2HAObservation;
  etcdReachable?: boolean;
  /** HAObservation preserves source time independently of response and transport time. */
  frigateObservation?: V2HAObservation;
  frigateReachable?: boolean;
  /** Role and write authority are last reported, qualified by role_observation. */
  haRole?: V2HARole;
  hostname?: string;
  /**
   * Id is the configured Frigate server ID or provisioned witness name.
   * The pair (kind, id) identifies a member; etcd names are separate.
   */
  id?: string;
  interfaces?: V2HANetworkInterface[];
  /** HAObservation preserves source time independently of response and transport time. */
  inventoryObservation?: V2HAObservation;
  /**
   * HAMemberRole identifies why a host participates in the HA control plane.
   *
   *  - HA_MEMBER_ROLE_FRIGATE: HA_MEMBER_ROLE_FRIGATE is a Frigate control-server peer.
   *  - HA_MEMBER_ROLE_WITNESS: HA_MEMBER_ROLE_WITNESS is an etcd-only voting witness.
   */
  kind?: V2HAMemberRole;
  local?: boolean;
  /**
   * HARecoveryStatus is bounded, machine-readable progress for one post-Patroni
   * recovery episode. It is process-local status backed by shared assignment
   * authority; it is not a durable workflow record.
   */
  recovery?: V2HARecoveryStatus;
  /** HAObservation preserves source time independently of response and transport time. */
  roleObservation?: V2HAObservation;
}

/** HANetworkInterface is observed hardware metadata, never a logical identity. */
export interface V2HANetworkInterface {
  ipAddresses?: string[];
  macAddress?: string;
  name?: string;
  primary?: boolean;
}

/** HAObservation preserves source time independently of response and transport time. */
export interface V2HAObservation {
  detail?: string;
  freshness?: V2HAObservationFreshness;
  /** @format date-time */
  lastSuccessfulAt?: string;
  /** @format date-time */
  observedAt?: string;
  sourceServerId?: string;
}

/** @default "HA_OBSERVATION_FRESHNESS_UNSPECIFIED" */
export enum V2HAObservationFreshness {
  HA_OBSERVATION_FRESHNESS_UNSPECIFIED = "HA_OBSERVATION_FRESHNESS_UNSPECIFIED",
  HA_OBSERVATION_FRESHNESS_FRESH = "HA_OBSERVATION_FRESHNESS_FRESH",
  HA_OBSERVATION_FRESHNESS_STALE = "HA_OBSERVATION_FRESHNESS_STALE",
}

/**
 * HARecoveryPhase identifies the highest-priority recovery work still in
 * progress. Values are ordered by the runtime sequence, not by severity.
 * @default "HA_RECOVERY_PHASE_UNSPECIFIED"
 */
export enum V2HARecoveryPhase {
  HA_RECOVERY_PHASE_UNSPECIFIED = "HA_RECOVERY_PHASE_UNSPECIFIED",
  HA_RECOVERY_PHASE_STEADY = "HA_RECOVERY_PHASE_STEADY",
  HA_RECOVERY_PHASE_DATABASE = "HA_RECOVERY_PHASE_DATABASE",
  HA_RECOVERY_PHASE_CONTROL_STATE = "HA_RECOVERY_PHASE_CONTROL_STATE",
  HA_RECOVERY_PHASE_ASSIGNMENTS = "HA_RECOVERY_PHASE_ASSIGNMENTS",
  HA_RECOVERY_PHASE_FLOTILLA_SYNC = "HA_RECOVERY_PHASE_FLOTILLA_SYNC",
}

/**
 * HARecoveryState describes whether this Frigate process has returned to a
 * trustworthy steady state after a Patroni primary transition.
 * @default "HA_RECOVERY_STATE_UNSPECIFIED"
 */
export enum V2HARecoveryState {
  HA_RECOVERY_STATE_UNSPECIFIED = "HA_RECOVERY_STATE_UNSPECIFIED",
  HA_RECOVERY_STATE_STEADY = "HA_RECOVERY_STATE_STEADY",
  HA_RECOVERY_STATE_RECONCILING = "HA_RECOVERY_STATE_RECONCILING",
  HA_RECOVERY_STATE_DEGRADED = "HA_RECOVERY_STATE_DEGRADED",
}

/**
 * HARecoveryStatus is bounded, machine-readable progress for one post-Patroni
 * recovery episode. It is process-local status backed by shared assignment
 * authority; it is not a durable workflow record.
 */
export interface V2HARecoveryStatus {
  assignmentCounts?: Record<string, number>;
  /** @format int64 */
  deferredFullSyncs?: number;
  detail?: string;
  /** @format int64 */
  expectedBoards?: number;
  historyDeferred?: boolean;
  /**
   * HARecoveryPhase identifies the highest-priority recovery work still in
   * progress. Values are ordered by the runtime sequence, not by severity.
   */
  phase?: V2HARecoveryPhase;
  primaryInstance?: string;
  /** @format int64 */
  reconciledBoards?: number;
  /** @format date-time */
  startedAt?: string;
  /**
   * HARecoveryState describes whether this Frigate process has returned to a
   * trustworthy steady state after a Patroni primary transition.
   */
  state?: V2HARecoveryState;
  /** @format date-time */
  updatedAt?: string;
}

/**
 * HARole identifies this server's role in a high-availability cluster.
 * @default "HA_ROLE_UNSPECIFIED"
 */
export enum V2HARole {
  HA_ROLE_UNSPECIFIED = "HA_ROLE_UNSPECIFIED",
  HA_ROLE_PRIMARY = "HA_ROLE_PRIMARY",
  HA_ROLE_STANDBY = "HA_ROLE_STANDBY",
  HA_ROLE_FENCED = "HA_ROLE_FENCED",
}

/** HAStatus is a bounded diagnostic view, not a command-authority grant. */
export interface V2HAStatus {
  /** ClusterId is shared durable identity, independent of etcd's cluster ID. */
  clusterId?: string;
  conflicts?: string[];
  detail?: string;
  enabled?: boolean;
  /** HAIdentityState describes agreement on a durable logical cluster identity. */
  identityState?: V2HAIdentityState;
  /**
   * InventoryReady means local persisted identity was successfully loaded.
   * False must never be interpreted as evidence that no prior identity exists.
   */
  inventoryReady?: boolean;
  members?: V2HAMemberStatus[];
  /** @format date-time */
  observedAt?: string;
  peerServerId?: string;
  reportingServerId?: string;
}

/** HardwareInfo contains hardware details. */
export interface V2HardwareInfo {
  /** ControlBoardInfo contains stable control-board hardware identity. */
  controlBoard?: V2ControlBoardInfo;
  coolingFans?: V2CoolingFanInfo[];
  model?: string;
  powerSupplies?: V2PowerSupplySummary[];
  /** @format int32 */
  psuCount?: number;
  /**
   * recorded_hashboards contains only static, display-only inventory evidence.
   * An empty collection means there is no recorded evidence; it does not
   * establish that the chassis currently contains no hashboards. Consumers
   * must use HashboardService for current presence.
   */
  recordedHashboards?: V2RecordedHashboardInventory[];
  /** @format int32 */
  slotCount?: number;
}

/**
 * HardwareRev mirrors the hashboard firmware hardware_revision enum
 * (lib/hashboard/lib/protobuf/protos/hashboard_cmd.proto) value for value, so
 * clients can compare it numerically against the advertised
 * Hashboard.hw_revision string without re-implementing hbmeta's
 * metadata-revision translation.
 * @default "HARDWARE_REV_UNSPECIFIED"
 */
export enum V2HardwareRev {
  HARDWARE_REV_UNSPECIFIED = "HARDWARE_REV_UNSPECIFIED",
  HARDWARE_REV_B2_PROTO_1 = "HARDWARE_REV_B2_PROTO_1",
  HARDWARE_REV_B2_EVT = "HARDWARE_REV_B2_EVT",
  HARDWARE_REV_B3A_DEV = "HARDWARE_REV_B3A_DEV",
  HARDWARE_REV_B3A_PROTO_1 = "HARDWARE_REV_B3A_PROTO_1",
  HARDWARE_REV_B3B_PROTO_1 = "HARDWARE_REV_B3B_PROTO_1",
  HARDWARE_REV_B3A_MP = "HARDWARE_REV_B3A_MP",
  HARDWARE_REV_B3B_MP = "HARDWARE_REV_B3B_MP",
  HARDWARE_REV_B4_192_PROTO_0 = "HARDWARE_REV_B4_192_PROTO_0",
  HARDWARE_REV_B4_192_PROTO_1 = "HARDWARE_REV_B4_192_PROTO_1",
  HARDWARE_REV_B4_128_PROTO_1 = "HARDWARE_REV_B4_128_PROTO_1",
  HARDWARE_REV_B4_192_PROTO_2 = "HARDWARE_REV_B4_192_PROTO_2",
  HARDWARE_REV_B4_128_PROTO_2 = "HARDWARE_REV_B4_128_PROTO_2",
  HARDWARE_REV_F1_PROTO_1A = "HARDWARE_REV_F1_PROTO_1A",
  HARDWARE_REV_F1_PROTO_1B = "HARDWARE_REV_F1_PROTO_1B",
  HARDWARE_REV_F1_EVT = "HARDWARE_REV_F1_EVT",
  HARDWARE_REV_F1_EVT2 = "HARDWARE_REV_F1_EVT2",
}

/** Hashboard represents a mining hashboard with its current state and health. */
export interface V2Hashboard {
  apiVersion?: string;
  /**
   * asic_bin is the complete board-level CP-bin summary decoded from ASIC
   * eFuse GRWVPL values. It is one exact TUI-compatible name (for example
   * "Z11" or "M04"), a catalogued V8 mixed-bin PCBA recipe label when its
   * distinct-bin presence signature matches exactly (for example "3A08MX1"),
   * "Mixed" for other complete valid mixtures, or empty when metadata is
   * unavailable, incomplete, or invalid. Recipe labels do not verify bin
   * multiplicity or placement.
   */
  asicBin?: string;
  asicModel?: string;
  chipId?: string;
  /** @format int64 */
  expectedAsicCount?: number;
  /**
   * Abbreviated commit hash extracted from the active firmware image's Git
   * describe identifier, for example "1a2b3c4". Populated only for boards
   * with a live local connection; empty when firmware metadata has not been read.
   */
  firmwareGitHash?: string;
  /** Semantic hashboard firmware version advertised over L2, for example "1.2.3". */
  firmwareVersion?: string;
  /** HashboardHealth contains real-time health metrics for a hashboard. */
  health?: V2HashboardHealth;
  hwRevision?: string;
  /**
   * isolated is true when this board is excluded from cluster start/stop and the
   * mining-intent reconciler. Set via MiningService.SetIsolation. Reflects only
   * the serving node's process-local isolation set; it is always false for
   * boards with source HASHBOARD_SOURCE_REMOTE even if the owning node has
   * isolated them.
   */
  isolated?: boolean;
  /** HashboardLocation describes the physical position of a hashboard. */
  location?: V2HashboardLocation;
  /**
   * mac_address is the legacy CT1 board identity. It is empty for R2 and must
   * never contain a synthesized address.
   */
  macAddress?: string;
  /** MiningState represents the operational state of a hashboard. */
  miningState?: V2MiningState;
  /**
   * Latest module ID as 16-character lowercase hex. Live advertisements take
   * precedence, with the shared last-known identity used across restart/HA.
   */
  moduleId?: string;
  /** @format int64 */
  observedAsicCount?: number;
  ownerServerId?: string;
  /** @format double */
  powerTargetMaxWatts?: number;
  /**
   * power_target_min_watts and power_target_max_watts are the latest valid
   * per-board tuning limits advertised by locally connected firmware.
   * @format double
   */
  powerTargetMinWatts?: number;
  /** psu_identity is durable module-level enrichment keyed by module_id. */
  psuIdentity?: V2PsuIdentity;
  /**
   * Power supply type reported by firmware health telemetry, for example
   * "BOCO" or "WELLTIP". Populated only for boards with a live local
   * connection; empty when the type has not been reported.
   */
  psuType?: string;
  /**
   * recovery is present while bounded automatic READY retries are active or
   * exhausted for a protected MC3 firmware recovery episode.
   */
  recovery?: V2HashboardRecovery;
  /**
   * serial is the stable physical identity for R2 and must be populated. CT1
   * retains its existing MAC identity and may also report a serial.
   */
  serial?: string;
  /** HashboardSource describes where the board state in this response came from. */
  source?: V2HashboardSource;
}

/** HashboardASICErrorState contains ASIC-specific detail for an error. */
export interface V2HashboardASICErrorState {
  /**
   * Id is the raw zero-based firmware ASIC ID. Firmware numbers only the
   * ASICs in its live topology, so on a board with an unpopulated or
   * non-enumerated stack it can differ from the physical ASIC index.
   * @format int64
   */
  id?: number;
  /**
   * PhysicalPosition is the owning Frigate's mapping of id onto the board's
   * physical ASIC layout at capture time. Its index uses the same physical
   * index space as ASIC.index. It is absent when the layout could not
   * resolve id unambiguously, from older Frigate releases, and for latched
   * last-shutdown errors whose firmware topology may predate the current one.
   */
  physicalPosition?: V2HashboardASICPhysicalPosition;
  /** @format double */
  temperature?: number;
  /** @format double */
  voltage?: number;
  /** HashboardVoltageDomain identifies the ASIC voltage domain involved in an error. */
  voltageDomain?: V2HashboardVoltageDomain;
}

/** HashboardASICLayout describes the physical ASIC layout for a hashboard. */
export interface V2HashboardASICLayout {
  /** @format int32 */
  asicsPerStack?: number;
  entries?: V2ASICLayoutEntry[];
  /** @format int32 */
  stackCount?: number;
  /** @format int32 */
  totalAsics?: number;
}

/**
 * HashboardASICPhysicalPosition locates one ASIC on the hashboard's physical
 * layout.
 */
export interface V2HashboardASICPhysicalPosition {
  /**
   * Index is the zero-based physical ASIC position, stack_id multiplied by
   * the layout's ASICs per stack plus stack_index.
   * @format int64
   */
  index?: number;
  /**
   * StackId is the zero-based physical stack; stack 0 is labeled A.
   * @format int64
   */
  stackId?: number;
  /**
   * StackIndex is the zero-based ASIC position within the stack.
   * @format int64
   */
  stackIndex?: number;
}

/**
 * HashboardASICThermalThrottleHealth is one firmware-reported FMargin
 * thermal-throttle snapshot. quarantined_asic_ids are compact firmware ASIC
 * identities and must be joined through ASIC.compact_index before display.
 */
export interface V2HashboardASICThermalThrottleHealth {
  /** @format int64 */
  asicCount?: number;
  quarantinedAsicIds?: number[];
}

/**
 * HashboardErrorCause identifies a firmware-reported or Frigate-derived
 * hashboard error or warning cause.
 *
 *  - HASHBOARD_ERROR_CAUSE_ASIC_THERMAL_THROTTLING: Frigate derives thermal throttling from asic_thermal_throttle; firmware
 * does not emit this value in its errors or warnings arrays.
 *  - HASHBOARD_ERROR_CAUSE_STACK_DROPOUT: Frigate derives a stack dropout warning when a stack that hashed in the
 * current mining session, whether enumerated at session start or joining
 * later, stops hashing while other stacks continue. The warning clears when
 * the stack hashes again. Firmware does not emit this value; it sits outside
 * the firmware numbering range so future firmware causes cannot collide
 * with it.
 * @default "HASHBOARD_ERROR_CAUSE_UNSPECIFIED"
 */
export enum V2HashboardErrorCause {
  HASHBOARD_ERROR_CAUSE_UNSPECIFIED = "HASHBOARD_ERROR_CAUSE_UNSPECIFIED",
  HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT = "HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT",
  HASHBOARD_ERROR_CAUSE_BOARD_OVER_VOLTAGE = "HASHBOARD_ERROR_CAUSE_BOARD_OVER_VOLTAGE",
  HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE = "HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE",
  HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE = "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE",
  HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY = "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY",
  HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY_OVER_TEMPERATURE = "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY_OVER_TEMPERATURE",
  HASHBOARD_ERROR_CAUSE_BOARD_EXCESSIVE_BUSBAR_VOLTAGE_DROP = "HASHBOARD_ERROR_CAUSE_BOARD_EXCESSIVE_BUSBAR_VOLTAGE_DROP",
  HASHBOARD_ERROR_CAUSE_BLACKBOX_STORAGE_FAULT = "HASHBOARD_ERROR_CAUSE_BLACKBOX_STORAGE_FAULT",
  HASHBOARD_ERROR_CAUSE_ASIC_OVER_VOLTAGE = "HASHBOARD_ERROR_CAUSE_ASIC_OVER_VOLTAGE",
  HASHBOARD_ERROR_CAUSE_ASIC_UNDER_VOLTAGE = "HASHBOARD_ERROR_CAUSE_ASIC_UNDER_VOLTAGE",
  HASHBOARD_ERROR_CAUSE_ASIC_OVER_TEMPERATURE = "HASHBOARD_ERROR_CAUSE_ASIC_OVER_TEMPERATURE",
  HASHBOARD_ERROR_CAUSE_ASIC_UNDER_TEMPERATURE = "HASHBOARD_ERROR_CAUSE_ASIC_UNDER_TEMPERATURE",
  HASHBOARD_ERROR_CAUSE_ASIC_ENUMERATION = "HASHBOARD_ERROR_CAUSE_ASIC_ENUMERATION",
  HASHBOARD_ERROR_CAUSE_ASIC_ECC = "HASHBOARD_ERROR_CAUSE_ASIC_ECC",
  HASHBOARD_ERROR_CAUSE_ASIC_CORNER_TEMPERATURE_DELTA = "HASHBOARD_ERROR_CAUSE_ASIC_CORNER_TEMPERATURE_DELTA",
  HASHBOARD_ERROR_CAUSE_ASIC_LEADING_EDGE_TEMPERATURE_DELTA = "HASHBOARD_ERROR_CAUSE_ASIC_LEADING_EDGE_TEMPERATURE_DELTA",
  HASHBOARD_ERROR_CAUSE_ASIC_TRAILING_EDGE_TEMPERATURE_DELTA = "HASHBOARD_ERROR_CAUSE_ASIC_TRAILING_EDGE_TEMPERATURE_DELTA",
  HASHBOARD_ERROR_CAUSE_ASIC_HOTSPOT_TEMPERATURE_DELTA = "HASHBOARD_ERROR_CAUSE_ASIC_HOTSPOT_TEMPERATURE_DELTA",
  HASHBOARD_ERROR_CAUSE_SOFTWARE_ERROR = "HASHBOARD_ERROR_CAUSE_SOFTWARE_ERROR",
  HASHBOARD_ERROR_CAUSE_USB_ERROR = "HASHBOARD_ERROR_CAUSE_USB_ERROR",
  HASHBOARD_ERROR_CAUSE_MC3_STARTUP_TIMEOUT = "HASHBOARD_ERROR_CAUSE_MC3_STARTUP_TIMEOUT",
  HASHBOARD_ERROR_CAUSE_MC3_STARTUP_FAILED = "HASHBOARD_ERROR_CAUSE_MC3_STARTUP_FAILED",
  HASHBOARD_ERROR_CAUSE_MC3_NO_STACK_RUNNING = "HASHBOARD_ERROR_CAUSE_MC3_NO_STACK_RUNNING",
  HASHBOARD_ERROR_CAUSE_MC3_RUNTIME_COMMS_BUSY_TIMEOUT = "HASHBOARD_ERROR_CAUSE_MC3_RUNTIME_COMMS_BUSY_TIMEOUT",
  HASHBOARD_ERROR_CAUSE_MC3_KNOWN_GOOD_STARTUP_DROP = "HASHBOARD_ERROR_CAUSE_MC3_KNOWN_GOOD_STARTUP_DROP",
  HASHBOARD_ERROR_CAUSE_ASIC_THERMAL_THROTTLING = "HASHBOARD_ERROR_CAUSE_ASIC_THERMAL_THROTTLING",
  HASHBOARD_ERROR_CAUSE_STACK_ENUMERATION = "HASHBOARD_ERROR_CAUSE_STACK_ENUMERATION",
  HASHBOARD_ERROR_CAUSE_STACK_DELTA_THERMAL_THROTTLE = "HASHBOARD_ERROR_CAUSE_STACK_DELTA_THERMAL_THROTTLE",
  HASHBOARD_ERROR_CAUSE_UNKNOWN_ERROR = "HASHBOARD_ERROR_CAUSE_UNKNOWN_ERROR",
  HASHBOARD_ERROR_CAUSE_STACK_DROPOUT = "HASHBOARD_ERROR_CAUSE_STACK_DROPOUT",
}

/** HashboardEvent is a single entry in a hashboard's event history. */
export interface V2HashboardEvent {
  /** where present on the cause. */
  asic?: V2HashboardASICErrorState;
  /** error/warning/last-shutdown only. */
  cause?: V2HashboardErrorCause;
  /**
   * occurrences within window (aggregated cause events).
   * @format int32
   */
  count?: number;
  /** human-readable summary. */
  detail?: string;
  /** state-change only. */
  fromState?: V2MiningState;
  /** isolation only. */
  isolatedBy?: string;
  /** isolation only. */
  isolationReason?: string;
  /**
   * error/warning/last-shutdown only.
   * @format date-time
   */
  lastSeen?: string;
  /**
   * raw zero-based firmware stack index, when reported.
   * @format int64
   */
  stack?: number;
  /**
   * For aggregated cause events, first-seen time.
   * @format date-time
   */
  time?: string;
  /** state-change only. */
  toState?: V2MiningState;
  /**
   * HashboardEventType categorizes an entry in a hashboard event history.
   *
   *  - HASHBOARD_EVENT_TYPE_STACK_DROPOUT: A stack that hashed earlier in the mining session, at session start or
   * after joining later, stopped hashing.
   */
  type?: V2HashboardEventType;
}

/**
 * HashboardEventType categorizes an entry in a hashboard event history.
 *
 *  - HASHBOARD_EVENT_TYPE_STACK_DROPOUT: A stack that hashed earlier in the mining session, at session start or
 * after joining later, stopped hashing.
 * @default "HASHBOARD_EVENT_TYPE_UNSPECIFIED"
 */
export enum V2HashboardEventType {
  HASHBOARD_EVENT_TYPE_UNSPECIFIED = "HASHBOARD_EVENT_TYPE_UNSPECIFIED",
  HASHBOARD_EVENT_TYPE_MINING_STATE_CHANGE = "HASHBOARD_EVENT_TYPE_MINING_STATE_CHANGE",
  HASHBOARD_EVENT_TYPE_ERROR = "HASHBOARD_EVENT_TYPE_ERROR",
  HASHBOARD_EVENT_TYPE_WARNING = "HASHBOARD_EVENT_TYPE_WARNING",
  HASHBOARD_EVENT_TYPE_ISOLATION = "HASHBOARD_EVENT_TYPE_ISOLATION",
  HASHBOARD_EVENT_TYPE_LAST_SHUTDOWN = "HASHBOARD_EVENT_TYPE_LAST_SHUTDOWN",
  HASHBOARD_EVENT_TYPE_STACK_DROPOUT = "HASHBOARD_EVENT_TYPE_STACK_DROPOUT",
}

/** HashboardHealth contains real-time health metrics for a hashboard. */
export interface V2HashboardHealth {
  /**
   * Observation time of the ASIC frame supplying the temperature summary.
   * @format date-time
   */
  asicTemperatureObservedAt?: string;
  /**
   * HashboardASICThermalThrottleHealth is one firmware-reported FMargin
   * thermal-throttle snapshot. quarantined_asic_ids are compact firmware ASIC
   * identities and must be joined through ASIC.compact_index before display.
   */
  asicThermalThrottle?: V2HashboardASICThermalThrottleHealth;
  /**
   * average_asic_temperature_c is absent when the source cannot distinguish
   * ASIC temperature from other board sensors.
   * @format double
   */
  averageAsicTemperatureC?: number;
  /** @format double */
  current?: number;
  errors?: V2HashboardHealthError[];
  /**
   * Mean ASIC Fbest headroom in MHz, present only after firmware FMargin has
   * produced a valid sample for the current controller run.
   * @format double
   */
  fmarginMeanMhz?: number;
  hashrailOk?: boolean;
  /** @format double */
  hashrate?: number;
  /** @format double */
  hashrateIdeal?: number;
  /**
   * Latched reason for the most recent mining shutdown. This is diagnostic
   * history and must not be counted as an active error.
   */
  lastShutdownError?: V2HashboardHealthError;
  /**
   * Maximum of reported ASIC temperatures, never board or PSU sensors.
   * @format double
   */
  maximumAsicTemperatureC?: number;
  /**
   * HashboardPerformanceTuningAlgorithm is the performance tuning algorithm
   * currently selected by the hashboard firmware. Presence on HashboardHealth
   * distinguishes a reported NONE value from legacy health telemetry.
   */
  performanceTuningAlgorithm?: V2HashboardPerformanceTuningAlgorithm;
  /** @format double */
  power?: number;
  /** PsuElectricalHealth contains one complete power-supply electrical snapshot. */
  psuElectrical?: V2PsuElectricalHealth;
  /** PsuThermalHealth contains the power supply's thermal telemetry. */
  psuThermal?: V2PsuThermalHealth;
  refclksOk?: boolean;
  temperatureSensors?: V2BoardTemperatureSensor[];
  temperatures?: number[];
  /** @format date-time */
  timestamp?: string;
  /**
   * Firmware RTOS uptime in milliseconds.
   * @format uint64
   */
  uptime?: string;
  /** @format double */
  voltage?: number;
  /**
   * HashboardVoltageTuningState is the firmware-reported automatic PSU voltage
   * tuning state. Presence on HashboardHealth distinguishes a reported DISABLED
   * state from a board that has not supplied health telemetry yet.
   */
  voltageTuningState?: V2HashboardVoltageTuningState;
  warnings?: V2HashboardHealthError[];
}

/**
 * HashboardHealthError is a firmware-reported or Frigate-derived error or
 * warning.
 */
export interface V2HashboardHealthError {
  /** HashboardASICErrorState contains ASIC-specific detail for an error. */
  asic?: V2HashboardASICErrorState;
  /**
   * HashboardErrorCause identifies a firmware-reported or Frigate-derived
   * hashboard error or warning cause.
   *
   *  - HASHBOARD_ERROR_CAUSE_ASIC_THERMAL_THROTTLING: Frigate derives thermal throttling from asic_thermal_throttle; firmware
   * does not emit this value in its errors or warnings arrays.
   *  - HASHBOARD_ERROR_CAUSE_STACK_DROPOUT: Frigate derives a stack dropout warning when a stack that hashed in the
   * current mining session, whether enumerated at session start or joining
   * later, stops hashing while other stacks continue. The warning clears when
   * the stack hashes again. Firmware does not emit this value; it sits outside
   * the firmware numbering range so future firmware causes cannot collide
   * with it.
   */
  cause?: V2HashboardErrorCause;
  /** @format int64 */
  firmwareTimestamp?: number;
  /**
   * Stack is the raw zero-based firmware stack index for stack-scoped errors.
   * @format int64
   */
  stack?: number;
}

/**
 * HashboardHistoricalIdentityEvidence is the last valid occupied identity
 * observed in one physical slot. It can be rendered while the current slot is
 * UNKNOWN or EMPTY, but it is never current authority. After a board moves, the
 * same serial_number can appear in more than one slot's history; board-catalog
 * consumers should retain the newest observed_at.
 */
export interface V2HashboardHistoricalIdentityEvidence {
  /** @format date-time */
  observedAt?: string;
  serialNumber?: string;
}

/**
 * HashboardIdentifyLEDMode is the operator-requested identification LED mode.
 * @default "HASHBOARD_IDENTIFY_LED_MODE_UNSPECIFIED"
 */
export enum V2HashboardIdentifyLEDMode {
  HASHBOARD_IDENTIFY_LED_MODE_UNSPECIFIED = "HASHBOARD_IDENTIFY_LED_MODE_UNSPECIFIED",
  HASHBOARD_IDENTIFY_LED_MODE_OFF = "HASHBOARD_IDENTIFY_LED_MODE_OFF",
  HASHBOARD_IDENTIFY_LED_MODE_BLINK = "HASHBOARD_IDENTIFY_LED_MODE_BLINK",
}

/** HashboardLocation describes the physical position of a hashboard. */
export interface V2HashboardLocation {
  expectedPowerFeedId?: string;
  /**
   * hashboard_position is the resolved one-based board position within a CT1
   * module. Values 1-3 are displayed as HB1-HB3. It is absent when Frigate
   * cannot place the board safely.
   * @format int64
   */
  hashboardPosition?: number;
  /**
   * hashboard_position_source describes whether the resolved position came
   * from firmware identity or the deterministic legacy MAC-order fallback.
   */
  hashboardPositionSource?: V2HashboardPositionSource;
  /** @format int32 */
  module?: number;
  powerFeedId?: string;
  /**
   * slot is the optional one-based R2 chassis slot.
   * @format int64
   */
  slot?: number;
  switchId?: string;
  switchPortId?: string;
  /** @format int32 */
  tank?: number;
  wiringReasons?: string[];
  wiringStatus?: string;
}

export interface V2HashboardObservedUptimeWindow {
  /** @format uint64 */
  observedTimeMs?: string;
  /** @format uint64 */
  poweredTimeMs?: string;
  /** @format int64 */
  windowDays?: number;
}

/**
 * HashboardPerformanceTuningAlgorithm is the performance tuning algorithm
 * currently selected by the hashboard firmware. Presence on HashboardHealth
 * distinguishes a reported NONE value from legacy health telemetry.
 * @default "HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_UNSPECIFIED"
 */
export enum V2HashboardPerformanceTuningAlgorithm {
  HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_UNSPECIFIED = "HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_UNSPECIFIED",
  HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_NONE = "HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_NONE",
  HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_VOLTAGE_IMBALANCE = "HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_VOLTAGE_IMBALANCE",
  HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_TEMPERATURE_GRADIENT = "HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_TEMPERATURE_GRADIENT",
  HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_TEMPERATURE_GRADIENT_WITH_VOLTAGE = "HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_TEMPERATURE_GRADIENT_WITH_VOLTAGE",
  HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_FMARGIN = "HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_FMARGIN",
  HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_FUZZING = "HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_FUZZING",
  HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_DISABLED = "HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_DISABLED",
  HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_FMARGIN_THERMAL_THROTTLE = "HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_FMARGIN_THERMAL_THROTTLE",
  HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_FMARGIN_DELTA_THERMAL_THROTTLE = "HASHBOARD_PERFORMANCE_TUNING_ALGORITHM_FMARGIN_DELTA_THERMAL_THROTTLE",
}

/**
 * HashboardPositionSource identifies the evidence used to place a CT1 board
 * within its three-board module.
 * @default "HASHBOARD_POSITION_SOURCE_UNSPECIFIED"
 */
export enum V2HashboardPositionSource {
  HASHBOARD_POSITION_SOURCE_UNSPECIFIED = "HASHBOARD_POSITION_SOURCE_UNSPECIFIED",
  HASHBOARD_POSITION_SOURCE_FIRMWARE = "HASHBOARD_POSITION_SOURCE_FIRMWARE",
  HASHBOARD_POSITION_SOURCE_MAC_ORDER = "HASHBOARD_POSITION_SOURCE_MAC_ORDER",
  HASHBOARD_POSITION_SOURCE_SWITCH_PORT = "HASHBOARD_POSITION_SOURCE_SWITCH_PORT",
}

/** HashboardPower contains power consumption data for a single hashboard. */
export interface V2HashboardPower {
  /**
   * current is board input current in amperes.
   * @format double
   */
  current?: number;
  /**
   * hashrate is board hashrate in terahashes per second.
   * @format double
   */
  hashrate?: number;
  /**
   * mac_address is the legacy CT1 board identity. It is empty for R2 and must
   * never contain a synthesized address.
   */
  macAddress?: string;
  /** serial is the stable R2 board identity. */
  serial?: string;
  /**
   * slot is the optional one-based R2 chassis slot.
   * @format int64
   */
  slot?: number;
  /**
   * voltage is board input voltage in volts.
   * @format double
   */
  voltage?: number;
  /**
   * watts is instantaneous board power in watts.
   * @format double
   */
  watts?: number;
}

/** HashboardRecovery is the shared HA-safe automatic READY retry state. */
export interface V2HashboardRecovery {
  /**
   * HashboardErrorCause identifies a firmware-reported or Frigate-derived
   * hashboard error or warning cause.
   *
   *  - HASHBOARD_ERROR_CAUSE_ASIC_THERMAL_THROTTLING: Frigate derives thermal throttling from asic_thermal_throttle; firmware
   * does not emit this value in its errors or warnings arrays.
   *  - HASHBOARD_ERROR_CAUSE_STACK_DROPOUT: Frigate derives a stack dropout warning when a stack that hashed in the
   * current mining session, whether enumerated at session start or joining
   * later, stops hashing while other stacks continue. The warning clears when
   * the stack hashes again. Firmware does not emit this value; it sits outside
   * the firmware numbering range so future firmware causes cannot collide
   * with it.
   */
  cause?: V2HashboardErrorCause;
  controllerServerId?: string;
  exhausted?: boolean;
  /** @format int64 */
  maxAttempts?: number;
  /** @format date-time */
  nextRetryAt?: string;
  /** @format int64 */
  retryCount?: number;
}

/** HashboardRunInConfig configures a single hashboard run-in task. */
export interface V2HashboardRunInConfig {
  /** RunInArtifactDetail controls the telemetry detail persisted in a result artifact. */
  artifactDetail?: V2RunInArtifactDetail;
  /** @format double */
  boardPowerTargetWatts?: number;
  cooldownDuration?: string;
  duration?: string;
  /** PerformanceSweep configures optional target-power sweep steps. */
  performanceSweep?: V2PerformanceSweep;
  sampleInterval?: string;
  /** HashboardRunInStopCriteria configures failure criteria. */
  stopCriteria?: V2HashboardRunInStopCriteria;
  stopMiningOnFinish?: boolean;
  /** TuningSweep configures fixed-voltage points crossed with either fixed-frequency or power-target/PLL-algorithm points. */
  tuningSweep?: V2TuningSweep;
}

/**
 * HashboardRunInStatus is the lifecycle state of a run-in task.
 * @default "HASHBOARD_RUN_IN_STATUS_UNSPECIFIED"
 */
export enum V2HashboardRunInStatus {
  HASHBOARD_RUN_IN_STATUS_UNSPECIFIED = "HASHBOARD_RUN_IN_STATUS_UNSPECIFIED",
  HASHBOARD_RUN_IN_STATUS_QUEUED = "HASHBOARD_RUN_IN_STATUS_QUEUED",
  HASHBOARD_RUN_IN_STATUS_RUNNING = "HASHBOARD_RUN_IN_STATUS_RUNNING",
  HASHBOARD_RUN_IN_STATUS_COOLDOWN = "HASHBOARD_RUN_IN_STATUS_COOLDOWN",
  HASHBOARD_RUN_IN_STATUS_COMPLETED = "HASHBOARD_RUN_IN_STATUS_COMPLETED",
  HASHBOARD_RUN_IN_STATUS_COMPLETED_WITH_WARNINGS = "HASHBOARD_RUN_IN_STATUS_COMPLETED_WITH_WARNINGS",
  HASHBOARD_RUN_IN_STATUS_FAILED = "HASHBOARD_RUN_IN_STATUS_FAILED",
  HASHBOARD_RUN_IN_STATUS_CANCELED = "HASHBOARD_RUN_IN_STATUS_CANCELED",
}

/** HashboardRunInStopCriteria configures failure criteria. */
export interface V2HashboardRunInStopCriteria {
  /** ASICStopCriteria applies to ASIC-level telemetry. */
  asic?: V2ASICStopCriteria;
  /** HashboardStopCriteria applies to board-level telemetry. */
  hashboard?: V2HashboardStopCriteria;
}

/** HashboardRunInTask is a status snapshot for a run-in task. */
export interface V2HashboardRunInTask {
  compressedSha256?: string;
  /**
   * Requested fixed PSU voltage for the active tuning-sweep point. Zero when
   * the task is not currently executing a fixed-voltage point.
   * @format int64
   */
  activeVoltageSetpointMillivolts?: number;
  /** RunInArtifactDetail controls the telemetry detail persisted in a result artifact. */
  artifactDetail?: V2RunInArtifactDetail;
  /** @format date-time */
  artifactFinalizedAt?: string;
  artifactState?: string;
  /** @format int32 */
  artifactVersion?: number;
  boards?: V2RunInTaskBoard[];
  /** @format int64 */
  compressedSize?: string;
  /** @format date-time */
  endedAt?: string;
  finalFilename?: string;
  macAddress?: string;
  moduleId?: string;
  originalIdentifier?: string;
  reason?: string;
  resultReady?: boolean;
  /** @format date-time */
  retentionExpiresAt?: string;
  serial?: string;
  serverId?: string;
  /** @format date-time */
  startedAt?: string;
  /** HashboardRunInStatus is the lifecycle state of a run-in task. */
  status?: V2HashboardRunInStatus;
  taskId?: string;
  /** @format int64 */
  uncompressedSize?: string;
  validationStage?: string;
  validationState?: string;
  voltageLeaderMac?: string;
}

/**
 * HashboardSlotState distinguishes incomplete observations from confirmed
 * empty and occupied chassis slots.
 * @default "HASHBOARD_SLOT_STATE_UNSPECIFIED"
 */
export enum V2HashboardSlotState {
  HASHBOARD_SLOT_STATE_UNSPECIFIED = "HASHBOARD_SLOT_STATE_UNSPECIFIED",
  HASHBOARD_SLOT_STATE_UNKNOWN = "HASHBOARD_SLOT_STATE_UNKNOWN",
  HASHBOARD_SLOT_STATE_EMPTY = "HASHBOARD_SLOT_STATE_EMPTY",
  HASHBOARD_SLOT_STATE_OCCUPIED = "HASHBOARD_SLOT_STATE_OCCUPIED",
}

/**
 * HashboardSource describes where the board state in this response came from.
 * @default "HASHBOARD_SOURCE_UNSPECIFIED"
 */
export enum V2HashboardSource {
  HASHBOARD_SOURCE_UNSPECIFIED = "HASHBOARD_SOURCE_UNSPECIFIED",
  HASHBOARD_SOURCE_LOCAL = "HASHBOARD_SOURCE_LOCAL",
  HASHBOARD_SOURCE_REMOTE = "HASHBOARD_SOURCE_REMOTE",
  HASHBOARD_SOURCE_INVENTORY = "HASHBOARD_SOURCE_INVENTORY",
}

/** HashboardStopCriteria applies to board-level telemetry. */
export interface V2HashboardStopCriteria {
  /** @format double */
  maxPower?: number;
  /** @format double */
  maxTemperature?: number;
  /** @format double */
  maxVoltage?: number;
  /** @format double */
  minHashrate?: number;
  /** @format double */
  minHashrateIdealRatio?: number;
  /** @format double */
  minVoltage?: number;
  /**
   * mining_dropout_duration is the continuous post-ramp zero-hash or
   * non-operational window before the run fails. Zero defaults to 30 seconds.
   */
  miningDropoutDuration?: string;
}

/**
 * HashboardTopology describes the bounded R2 chassis topology. The whole
 * message is omitted for products without a fixed slot topology. capacity is
 * the nonzero number of physical slots. generation changes whenever capacity,
 * current slot state, or current serial identity changes; a last_known-only
 * update does not change generation. complete is true only when slots contains
 * exactly one EMPTY or OCCUPIED observation for every slot from 1 through
 * capacity. Each OCCUPIED entry must match exactly one Hashboard in the same
 * ListHashboardsResponse by serial, and its HashboardLocation.slot must agree.
 * When complete is false, an unreported slot is UNKNOWN and must never be
 * inferred to be EMPTY.
 */
export interface V2HashboardTopology {
  /** @format int64 */
  capacity?: number;
  complete?: boolean;
  /** @format uint64 */
  generation?: string;
  slots?: V2HashboardTopologySlot[];
}

/**
 * HashboardTopologySlot is one one-based chassis slot observation. slot must be
 * within [1, HashboardTopology.capacity], and a topology must not contain the
 * same slot more than once. UNSPECIFIED is never valid in an emitted slot.
 * OCCUPIED requires serial_number; UNKNOWN and EMPTY require it to be empty.
 */
export interface V2HashboardTopologySlot {
  /**
   * last_known is historical evidence only. It is emitted only for UNKNOWN or
   * EMPTY slots because an OCCUPIED slot already carries current identity.
   */
  lastKnown?: V2HashboardHistoricalIdentityEvidence;
  serialNumber?: string;
  /** @format int64 */
  slot?: number;
  /**
   * HashboardSlotState distinguishes incomplete observations from confirmed
   * empty and occupied chassis slots.
   */
  state?: V2HashboardSlotState;
}

/**
 * HashboardVoltageDomain identifies the ASIC voltage domain involved in an error.
 * @default "HASHBOARD_VOLTAGE_DOMAIN_UNSPECIFIED"
 */
export enum V2HashboardVoltageDomain {
  HASHBOARD_VOLTAGE_DOMAIN_UNSPECIFIED = "HASHBOARD_VOLTAGE_DOMAIN_UNSPECIFIED",
  HASHBOARD_VOLTAGE_DOMAIN_VDD = "HASHBOARD_VOLTAGE_DOMAIN_VDD",
  HASHBOARD_VOLTAGE_DOMAIN_VDD_VDDI3 = "HASHBOARD_VOLTAGE_DOMAIN_VDD_VDDI3",
  HASHBOARD_VOLTAGE_DOMAIN_VDDI3_VDDI2 = "HASHBOARD_VOLTAGE_DOMAIN_VDDI3_VDDI2",
  HASHBOARD_VOLTAGE_DOMAIN_VDDI2_VDDI1 = "HASHBOARD_VOLTAGE_DOMAIN_VDDI2_VDDI1",
  HASHBOARD_VOLTAGE_DOMAIN_VDDI1 = "HASHBOARD_VOLTAGE_DOMAIN_VDDI1",
  HASHBOARD_VOLTAGE_DOMAIN_VDD_VDDI1 = "HASHBOARD_VOLTAGE_DOMAIN_VDD_VDDI1",
  HASHBOARD_VOLTAGE_DOMAIN_VDDI3_VSS = "HASHBOARD_VOLTAGE_DOMAIN_VDDI3_VSS",
}

/**
 * HashboardVoltageTuningState is the firmware-reported automatic PSU voltage
 * tuning state. Presence on HashboardHealth distinguishes a reported DISABLED
 * state from a board that has not supplied health telemetry yet.
 * @default "HASHBOARD_VOLTAGE_TUNING_STATE_UNSPECIFIED"
 */
export enum V2HashboardVoltageTuningState {
  HASHBOARD_VOLTAGE_TUNING_STATE_UNSPECIFIED = "HASHBOARD_VOLTAGE_TUNING_STATE_UNSPECIFIED",
  HASHBOARD_VOLTAGE_TUNING_STATE_DISABLED = "HASHBOARD_VOLTAGE_TUNING_STATE_DISABLED",
  HASHBOARD_VOLTAGE_TUNING_STATE_INIT = "HASHBOARD_VOLTAGE_TUNING_STATE_INIT",
  HASHBOARD_VOLTAGE_TUNING_STATE_SWEEP = "HASHBOARD_VOLTAGE_TUNING_STATE_SWEEP",
  HASHBOARD_VOLTAGE_TUNING_STATE_TUNING = "HASHBOARD_VOLTAGE_TUNING_STATE_TUNING",
  HASHBOARD_VOLTAGE_TUNING_STATE_STABILIZED = "HASHBOARD_VOLTAGE_TUNING_STATE_STABILIZED",
}

/**
 * HealthStatus represents the overall health of the system.
 * @default "HEALTH_STATUS_UNSPECIFIED"
 */
export enum V2HealthStatus {
  HEALTH_STATUS_UNSPECIFIED = "HEALTH_STATUS_UNSPECIFIED",
  HEALTH_STATUS_HEALTHY = "HEALTH_STATUS_HEALTHY",
  HEALTH_STATUS_DEGRADED = "HEALTH_STATUS_DEGRADED",
  HEALTH_STATUS_UNHEALTHY = "HEALTH_STATUS_UNHEALTHY",
}

/**
 * HistorySource identifies the retained resolution used for a response bucket.
 * @default "HISTORY_SOURCE_UNSPECIFIED"
 */
export enum V2HistorySource {
  HISTORY_SOURCE_UNSPECIFIED = "HISTORY_SOURCE_UNSPECIFIED",
  HISTORY_SOURCE_RAW = "HISTORY_SOURCE_RAW",
  HISTORY_SOURCE_HOURLY = "HISTORY_SOURCE_HOURLY",
  HISTORY_SOURCE_MIXED = "HISTORY_SOURCE_MIXED",
}

/**
 * HostCPUQuota reports the Frigate service's cgroup v2 CPU bandwidth limit and
 * cumulative throttling counters.
 */
export interface V2HostCPUQuota {
  /** @format uint64 */
  nrPeriods?: string;
  /** @format uint64 */
  nrThrottled?: string;
  /** @format uint64 */
  periodUsec?: string;
  /**
   * QuotaCores is cpu.max quota divided by period. Absent means unlimited.
   * @format double
   */
  quotaCores?: number;
  /**
   * RecentThrottledRatio is the fraction of enforcement periods throttled over
   * the most recent completed sampling window. Absent until a window completes.
   * @format double
   */
  recentThrottledRatio?: number;
  /** @format int64 */
  recentWindowSeconds?: number;
  /** @format uint64 */
  throttledUsec?: string;
}

/**
 * HostDiagnostics contains bounded host signals that remain useful when the
 * application or database is degraded. Presence distinguishes an observed
 * false value from a platform that cannot report the signal.
 */
export interface V2HostDiagnostics {
  clockSynchronized?: boolean;
  /**
   * CoredumpCount counts crash dumps retained by systemd-coredump.
   * @format int64
   */
  coredumpCount?: number;
  /**
   * CpuFrequencyMhz is CPU0's current scaling frequency.
   * @format double
   */
  cpuFrequencyMhz?: number;
  /**
   * CpuMaxFrequencyMhz is CPU0's maximum scaling frequency.
   * @format double
   */
  cpuMaxFrequencyMhz?: number;
  /** CpuQuota is the cgroup v2 CPU bandwidth state of the Frigate service. */
  cpuQuota?: V2HostCPUQuota;
  /**
   * HostBootId is the kernel boot ID from /proc/sys/kernel/random/boot_id.
   * Per-boot counters such as kernel_block_io_errors rebase when it changes.
   */
  hostBootId?: string;
  /**
   * KernelBlockIoErrors counts kernel I/O error messages for any block device
   * (NVMe, SD card, or USB storage) logged during the host boot named by
   * host_boot_id. It is absent unless the sample belongs to that boot.
   * @format uint64
   */
  kernelBlockIoErrors?: string;
  /** Nvme contains the most recent fresh SMART sample for each NVMe controller. */
  nvme?: V2HostNVMeHealth[];
  /** @format date-time */
  observedAt?: string;
  rootFilesystemReadOnly?: boolean;
  /**
   * SocTemperatureC is the SoC thermal-zone temperature in degrees Celsius.
   * @format double
   */
  socTemperatureC?: number;
  /** Throttle is the Raspberry Pi firmware throttle state. Other platforms omit it. */
  throttle?: V2HostThrottleState;
}

/** HostNVMeHealth is one NVMe controller's SMART health summary. */
export interface V2HostNVMeHealth {
  /** @format int64 */
  criticalWarning?: number;
  device?: string;
  /** @format uint64 */
  errorLogEntries?: string;
  /** @format uint64 */
  mediaErrors?: string;
  /** @format date-time */
  observedAt?: string;
  /** @format int64 */
  percentageUsed?: number;
  /**
   * SmartUnavailable reports that the controller is present but smartctl
   * returned no health log in the latest sample. The counters are then unknown
   * and left zero.
   */
  smartUnavailable?: boolean;
}

/** HostThrottleFlags decodes one half of the Raspberry Pi get_throttled bitmap. */
export interface V2HostThrottleFlags {
  frequencyCapped?: boolean;
  softTemperatureLimit?: boolean;
  throttled?: boolean;
  underVoltage?: boolean;
}

/** HostThrottleState reports current and since-boot Raspberry Pi throttling. */
export interface V2HostThrottleState {
  /** HostThrottleFlags decodes one half of the Raspberry Pi get_throttled bitmap. */
  current?: V2HostThrottleFlags;
  /**
   * Raw is the firmware bitmap for flags this schema does not name.
   * @format int64
   */
  raw?: number;
  /** HostThrottleFlags decodes one half of the Raspberry Pi get_throttled bitmap. */
  sinceBoot?: V2HostThrottleFlags;
}

/** IsolatedBoard describes one isolated hashboard. */
export interface V2IsolatedBoard {
  /**
   * desired_pll_algorithm is the requested PLL algorithm recorded alongside
   * desired_watts. It is not firmware readback of the effective algorithm.
   */
  desiredPllAlgorithm?: V2PLLAlgorithm;
  /**
   * desired_watts, when present, is the scoped manual per-board target recorded
   * in the shared isolation ledger.
   * @format double
   */
  desiredWatts?: number;
  /**
   * enforced reports whether this node is actively enforcing the isolation.
   * It is false when no current node manages or owns the ledger-isolated board.
   */
  enforced?: boolean;
  /**
   * isolated_at is when the isolation was recorded.
   * @format date-time
   */
  isolatedAt?: string;
  /** isolated_by is the server that recorded the isolation. */
  isolatedBy?: string;
  macAddress?: string;
  /**
   * 0 if unknown.
   * @format int32
   */
  module?: number;
  /**
   * reason is the operator-entered note recorded when the board was isolated
   * (e.g. who has claimed the unit and why).
   */
  reason?: string;
  /** Empty if unknown. */
  serial?: string;
  /**
   * 0 if unknown.
   * @format int32
   */
  tank?: number;
}

export interface V2ListAlertsResponse {
  alerts?: V2Alert[];
  /** PageResponse contains pagination metadata. */
  page?: V2PageResponse;
}

export interface V2ListAuditEventsResponse {
  events?: V2AuditEvent[];
}

export interface V2ListContainerPlantAlarmEpisodesResponse {
  episodes?: V2ContainerPlantAlarmEpisode[];
  nextPageToken?: string;
}

export interface V2ListContainerPlantEventsResponse {
  events?: V2ContainerPlantEvent[];
}

export interface V2ListFeedMappingRunEventsResponse {
  events?: V2FeedMappingRunEvent[];
  runId?: string;
}

export interface V2ListFirmwarePolicyBoardsResponse {
  boards?: V2FirmwarePolicyBoard[];
  policy?: V2FirmwarePolicy;
}

export interface V2ListFirmwareUpdatesResponse {
  updates?: V2FirmwareUpdate[];
}

export interface V2ListHashboardPowerResponse {
  hashboards?: V2HashboardPower[];
}

export interface V2ListHashboardRunInTasksResponse {
  complete?: boolean;
  sources?: V2RunInSourceCoverage[];
  tasks?: V2HashboardRunInTask[];
}

export interface V2ListHashboardsResponse {
  hashboards?: V2Hashboard[];
  /**
   * HashboardTopology describes the bounded R2 chassis topology. The whole
   * message is omitted for products without a fixed slot topology. capacity is
   * the nonzero number of physical slots. generation changes whenever capacity,
   * current slot state, or current serial identity changes; a last_known-only
   * update does not change generation. complete is true only when slots contains
   * exactly one EMPTY or OCCUPIED observation for every slot from 1 through
   * capacity. Each OCCUPIED entry must match exactly one Hashboard in the same
   * ListHashboardsResponse by serial, and its HashboardLocation.slot must agree.
   * When complete is false, an unreported slot is UNKNOWN and must never be
   * inferred to be EMPTY.
   */
  topology?: V2HashboardTopology;
}

export interface V2ListIsolationResponse {
  boards?: V2IsolatedBoard[];
}

export interface V2ListManufacturingBoardsResponse {
  boards?: V2ManufacturingBoard[];
  requireAdoption?: boolean;
}

export interface V2ListManufacturingSelfTestsResponse {
  tests?: V2ManufacturingSelfTest[];
}

export interface V2ListMiningGroupMembershipsResponse {
  memberships?: V2MiningGroupMembershipRecord[];
}

export interface V2ListMiningGroupPoolsResponse {
  pools?: V2MiningGroupPoolRecord[];
}

export interface V2ListMiningGroupsResponse {
  groups?: V2MiningGroupRecord[];
  inventoryIssues?: V2MiningGroupInventoryIssue[];
  pendingOperations?: V2MiningGroupOperationRecord[];
}

export interface V2ListModuleVoltageSettingsResponse {
  settings?: V2ModuleVoltageSetting[];
}

export interface V2ListPSUFirmwareCapsulesResponse {
  capsules?: V2PSUFirmwareCapsule[];
}

export interface V2ListPSUFirmwareUpdatesResponse {
  updates?: V2PSUFirmwareUpdate[];
}

export interface V2ListPoolsResponse {
  pools?: V2Pool[];
}

export interface V2ListPowerFeedsResponse {
  powerFeeds?: V2PowerFeed[];
  /**
   * snapshot_ready distinguishes a successful empty controller poll from the
   * startup window before the container monitor has completed its first poll.
   * Optional presence keeps older servers distinguishable from an explicit
   * false value.
   */
  snapshotReady?: boolean;
}

export interface V2ListPowerSuppliesResponse {
  powerSupplies?: V2PowerSupply[];
}

export interface V2ListRecoveryEvidenceResponse {
  evidence?: V2RecoveryEvidenceSummary[];
}

export interface V2ListTagsResponse {
  tags?: MdkV2Tag[];
}

export interface V2ListUnitTagsResponse {
  tags?: V2UnitTag[];
}

export interface V2LocateDeviceRequest {
  enable?: boolean;
}

export interface V2LocateDeviceResponse {
  active?: boolean;
}

/**
 * LockState is the hardware lock state reported by the device. It is
 * independent of any effective secure-policy override. Devices without an
 * initialized hardware lock are reported as UNINITIALIZED, while failed,
 * missing, or unrecognized observations are reported as UNKNOWN.
 *
 *  - LOCK_STATE_UNKNOWN: UNKNOWN is the intentional wire default because a missing observation has
 * the same semantics as an unrecognized hardware state. Consumers must not
 * treat UNKNOWN as unlocked.
 * buf:lint:ignore ENUM_ZERO_VALUE_SUFFIX
 * @default "LOCK_STATE_UNKNOWN"
 */
export enum V2LockState {
  LOCK_STATE_UNKNOWN = "LOCK_STATE_UNKNOWN",
  LOCK_STATE_OPEN = "LOCK_STATE_OPEN",
  LOCK_STATE_CLOSED = "LOCK_STATE_CLOSED",
  LOCK_STATE_UNLOCKED = "LOCK_STATE_UNLOCKED",
  LOCK_STATE_UNINITIALIZED = "LOCK_STATE_UNINITIALIZED",
}

/**
 * LogLevel represents the severity level for log filtering.
 * @default "LOG_LEVEL_UNSPECIFIED"
 */
export enum V2LogLevel {
  LOG_LEVEL_UNSPECIFIED = "LOG_LEVEL_UNSPECIFIED",
  LOG_LEVEL_DEBUG = "LOG_LEVEL_DEBUG",
  LOG_LEVEL_INFO = "LOG_LEVEL_INFO",
  LOG_LEVEL_WARNING = "LOG_LEVEL_WARNING",
  LOG_LEVEL_ERROR = "LOG_LEVEL_ERROR",
}

export interface V2LoginRequest {
  password?: string;
  username?: string;
}

export interface V2LoginResponse {
  /**
   * expires_at is the access-token expiry retained for protocol compatibility.
   * @format date-time
   */
  expiresAt?: string;
  refreshToken?: string;
  /** @format date-time */
  refreshTokenExpiresAt?: string;
  /** token is the access token retained for protocol compatibility. */
  token?: string;
}

export type V2LogoutResponse = object;

/**
 * MQTTSourceConnectionConfig reports nonsecret MQTT source settings. The
 * password value is never returned through this API.
 */
export interface V2MQTTSourceConnectionConfig {
  brokers?: MQTTSourceConnectionConfigBroker[];
  enabled?: boolean;
  passwordConfigured?: boolean;
  /** @format int64 */
  qos?: number;
  topic?: string;
  username?: string;
}

export interface V2ManagedBool {
  managed?: boolean;
  value?: boolean;
}

export interface V2ManagedDouble {
  managed?: boolean;
  /** @format double */
  value?: number;
}

export interface V2ManagedDuration {
  managed?: boolean;
  value?: string;
}

export interface V2ManagedErrorCauses {
  causes?: V2HashboardErrorCause[];
  managed?: boolean;
  /** Legacy numeric values retained for mixed-version Frigate deployments. */
  values?: number[];
}

export interface V2ManagedOperationalPolicy {
  acDisabledLoadThresholdKw?: V2ManagedDouble;
  insideAmbientCriticalC?: V2ManagedDouble;
  insideAmbientHysteresisC?: V2ManagedDouble;
  /**
   * Container thermal alarm thresholds. Older Frigate releases ignore these
   * fields and report thermal_alarms_active false, so Flotilla shows the
   * profile as inactive rather than silently applied.
   */
  insideAmbientWarningC?: V2ManagedDouble;
  maxBoardWatts?: V2ManagedDouble;
  minBoardWatts?: V2ManagedDouble;
  recoveryCauses?: V2ManagedErrorCauses;
  recoveryEnabled?: V2ManagedBool;
  recoveryHoldoff?: V2ManagedDuration;
  recoveryOffDuration?: V2ManagedDuration;
  safetyMargin?: V2ManagedDouble;
}

export interface V2ManufacturingASICResult {
  enumerated?: boolean;
  /** @format int64 */
  index?: number;
  /** @format int64 */
  stack?: number;
  /** @format int64 */
  stackIndex?: number;
  /** @format int64 */
  x?: number;
  /** @format int64 */
  y?: number;
}

export interface V2ManufacturingBISTReading {
  /** @format float */
  asicVdd?: number;
  /** @format int64 */
  domain?: number;
  /** @format float */
  vddh?: number;
  /** @format float */
  vddio?: number;
}

export interface V2ManufacturingBoard {
  activeTestId?: string;
  adopted?: boolean;
  /** @format date-time */
  adoptedAt?: string;
  adoptedBy?: string;
  flashing?: boolean;
  isolated?: boolean;
  /**
   * ManufacturingSelfTestSummary keeps inventory polling bounded; complete
   * evidence is returned by GetManufacturingSelfTest.
   */
  latestTest?: V2ManufacturingSelfTestSummary;
  macAddress?: string;
  /** MiningState represents the operational state of a hashboard. */
  miningState?: V2MiningState;
  reserved?: boolean;
  selfTesting?: boolean;
  /** serial is the persisted hashboard PCBA identity for this MAC when known. */
  serial?: string;
}

export interface V2ManufacturingSelfTest {
  actor?: string;
  asics?: V2ManufacturingASICResult[];
  bist?: V2ManufacturingBISTReading[];
  /** @format date-time */
  completedAt?: string;
  /** @format float */
  currentAfterBalance?: number;
  /** @format float */
  currentBeforeBalance?: number;
  /** @format float */
  currentDelta?: number;
  error?: string;
  failureReasons?: string[];
  log?: V2ManufacturingSelfTestLogEntry[];
  macAddress?: string;
  /** @format float */
  minimumCurrentDelta?: number;
  /** @format int64 */
  profileVersion?: number;
  pvtReady?: boolean;
  reachedRunning?: boolean;
  shutdownComplete?: boolean;
  stacks?: V2ManufacturingStackResult[];
  /** @format date-time */
  startedAt?: string;
  state?: V2ManufacturingSelfTestState;
  testId?: string;
  topology?: string;
  verdict?: V2ManufacturingSelfTestVerdict;
}

export interface V2ManufacturingSelfTestLogEntry {
  /** @format date-time */
  at?: string;
  message?: string;
}

/** @default "MANUFACTURING_SELF_TEST_STATE_UNSPECIFIED" */
export enum V2ManufacturingSelfTestState {
  MANUFACTURING_SELF_TEST_STATE_UNSPECIFIED = "MANUFACTURING_SELF_TEST_STATE_UNSPECIFIED",
  MANUFACTURING_SELF_TEST_STATE_QUEUED = "MANUFACTURING_SELF_TEST_STATE_QUEUED",
  MANUFACTURING_SELF_TEST_STATE_RUNNING = "MANUFACTURING_SELF_TEST_STATE_RUNNING",
  MANUFACTURING_SELF_TEST_STATE_COMPLETED = "MANUFACTURING_SELF_TEST_STATE_COMPLETED",
  MANUFACTURING_SELF_TEST_STATE_ERROR = "MANUFACTURING_SELF_TEST_STATE_ERROR",
}

/**
 * ManufacturingSelfTestSummary keeps inventory polling bounded; complete
 * evidence is returned by GetManufacturingSelfTest.
 */
export interface V2ManufacturingSelfTestSummary {
  actor?: string;
  /** @format date-time */
  completedAt?: string;
  error?: string;
  macAddress?: string;
  /** @format int64 */
  profileVersion?: number;
  /** @format date-time */
  startedAt?: string;
  state?: V2ManufacturingSelfTestState;
  testId?: string;
  verdict?: V2ManufacturingSelfTestVerdict;
}

/** @default "MANUFACTURING_SELF_TEST_VERDICT_UNSPECIFIED" */
export enum V2ManufacturingSelfTestVerdict {
  MANUFACTURING_SELF_TEST_VERDICT_UNSPECIFIED = "MANUFACTURING_SELF_TEST_VERDICT_UNSPECIFIED",
  MANUFACTURING_SELF_TEST_VERDICT_PASS = "MANUFACTURING_SELF_TEST_VERDICT_PASS",
  MANUFACTURING_SELF_TEST_VERDICT_FAIL = "MANUFACTURING_SELF_TEST_VERDICT_FAIL",
  MANUFACTURING_SELF_TEST_VERDICT_UNSUPPORTED = "MANUFACTURING_SELF_TEST_VERDICT_UNSUPPORTED",
}

export interface V2ManufacturingStackResult {
  /** @format int64 */
  enumeratedAsicCount?: number;
  /** @format int64 */
  expectedAsicCount?: number;
  matched?: boolean;
  /** @format int64 */
  stack?: number;
}

/**
 * MiningCurrentMeasurement distinguishes missing readings from measured zero.
 * complete means every board in the indicated roster has a current valid value.
 */
export interface V2MiningCurrentMeasurement {
  complete?: boolean;
  /** @format int64 */
  expectedBoards?: number;
  /** @format date-time */
  newestObservedAt?: string;
  /** @format date-time */
  oldestObservedAt?: string;
  /** @format int64 */
  reportingBoards?: number;
  /** @format double */
  value?: number;
}

/**
 * MiningCurrentMeasurements reuses the health read used for board counts.
 * Temperatures weight reporting boards equally; board temperature first takes
 * the mean of its finite sensors. ASIC temperature uses each board's persisted
 * ASIC-frame mean and its independent observation time.
 */
export interface V2MiningCurrentMeasurements {
  /**
   * MiningCurrentMeasurement distinguishes missing readings from measured zero.
   * complete means every board in the indicated roster has a current valid value.
   */
  averageAsicTemperatureC?: V2MiningCurrentMeasurement;
  /**
   * MiningCurrentMeasurement distinguishes missing readings from measured zero.
   * complete means every board in the indicated roster has a current valid value.
   */
  averageHashboardTemperatureC?: V2MiningCurrentMeasurement;
  /** @format date-time */
  evaluatedAt?: string;
  /**
   * MiningCurrentMeasurement distinguishes missing readings from measured zero.
   * complete means every board in the indicated roster has a current valid value.
   */
  hashboardPowerW?: V2MiningCurrentMeasurement;
  /** @format int64 */
  maxAgeSeconds?: number;
  /**  - MINING_MEASUREMENT_SCOPE_LOCAL_SNAPSHOTS: The fallback roster is process-local and cannot establish HA-wide coverage. */
  scope?: V2MiningMeasurementScope;
  source?: V2MiningMeasurementSource;
}

export interface V2MiningGroupBoardTelemetryRecord {
  /** @format double */
  allocatedBoardWatts?: number;
  /** @format double */
  appliedBoardWatts?: number;
  /** @format date-time */
  assignedAt?: string;
  availabilityReason?: string;
  /** @format double */
  boardPowerWatts?: number;
  /** @format double */
  efficiencyJTh?: number;
  groupId?: string;
  /** @format int64 */
  hashingTimeMs?: string;
  /** @format double */
  hashrateTh?: number;
  macAddress?: string;
  /** @format int64 */
  membershipGeneration?: string;
  miningAvailable?: boolean;
  /** @format date-time */
  observedAt?: string;
  /** @format int64 */
  observedTimeMs?: string;
  /** @format int64 */
  poweredOnTimeMs?: string;
  present?: boolean;
  reporting?: boolean;
}

export interface V2MiningGroupCapability {
  assignmentGranularity?: string;
  enabled?: boolean;
  /** @format int64 */
  maxBoardsPerGroup?: number;
  /** @format int64 */
  maxGroups?: number;
  /** @format int64 */
  maxPoolsPerGroup?: number;
  name?: string;
  /** @format int64 */
  stateRevision?: string;
  supportedPllAlgorithms?: string[];
  suspended?: boolean;
  /** @format int64 */
  version?: number;
}

export interface V2MiningGroupHistoryBucket {
  /** @format double */
  boardPowerWatts?: number;
  /** @format double */
  efficiencyJTh?: number;
  /** @format int64 */
  expectedBoards?: number;
  /** @format int64 */
  hashingTimeMs?: string;
  /** @format double */
  hashrateTh?: number;
  /**
   * Summed ownership-scoped board time, not group wall-clock uptime.
   * @format int64
   */
  observedTimeMs?: string;
  partialCoverage?: boolean;
  /** @format int64 */
  poweredOnTimeMs?: string;
  /** @format int64 */
  reportingBoards?: number;
  source?: string;
  /** @format date-time */
  start?: string;
}

export interface V2MiningGroupInventoryIssue {
  /** @format int64 */
  currentBoards?: number;
  groupId?: string;
  macAddress?: string;
  physicalModuleId?: string;
  powerFeedIds?: string[];
  reason?: string;
  /** @format int64 */
  retainedBoards?: number;
}

export interface V2MiningGroupMembershipRecord {
  /** @format date-time */
  assignedAt?: string;
  /** @format int64 */
  generation?: string;
  groupId?: string;
  macAddress?: string;
}

/** Writable customer metadata only; operating policy uses the intent RPC. */
export interface V2MiningGroupMetadata {
  externalRef?: string;
  name?: string;
}

export interface V2MiningGroupOperationRecord {
  destinationGroupId?: string;
  error?: string;
  id?: string;
  kind?: string;
  macAddresses?: string[];
  phase?: string;
  /** @format date-time */
  requestedAt?: string;
  sourceGroupId?: string;
  stopBlockers?: V2MiningGroupStopBlocker[];
  /** @format int64 */
  stopBoardsConfirmed?: number;
  /** @format int64 */
  stopBoardsTotal?: number;
  /** @format date-time */
  updatedAt?: string;
}

export interface V2MiningGroupPoolInput {
  /** Write-only; absent preserves an existing pool's credential. */
  password?: string;
  pool?: V2MiningGroupPoolRecord;
}

export interface V2MiningGroupPoolRecord {
  enabled?: boolean;
  /** @format int32 */
  id?: number;
  name?: string;
  /** @format int32 */
  priority?: number;
  /** @format double */
  shareWeight?: number;
  url?: string;
  username?: string;
}

export interface V2MiningGroupPoweredBoard {
  firmwareState?: string;
  hashrailOn?: boolean;
  macAddress?: string;
  /** @format date-time */
  observedAt?: string;
  ownerServerId?: string;
  refclksOn?: boolean;
}

export interface V2MiningGroupRecord {
  /** @format date-time */
  createdAt?: string;
  /** @format date-time */
  deletedAt?: string;
  desiredMining?: boolean;
  externalRef?: string;
  id?: string;
  name?: string;
  pllAlgorithm?: string;
  /** @format double */
  requestedBoardWatts?: number;
  /** @format int64 */
  resetGeneration?: string;
  /** @format int64 */
  revision?: string;
  /** @format date-time */
  updatedAt?: string;
}

export interface V2MiningGroupStopBlocker {
  /** @format date-time */
  lastSeenAt?: string;
  macAddress?: string;
  powerFeedId?: string;
}

export interface V2MiningGroupTelemetryRecord {
  /** @format double */
  allocatedBoardWatts?: number;
  /** @format double */
  appliedBoardWatts?: number;
  availabilityReason?: string;
  /** @format double */
  boardPowerWatts?: number;
  /** @format double */
  containerBudgetWatts?: number;
  /** @format double */
  efficiencyJTh?: number;
  /** @format int64 */
  expectedBoards?: number;
  /** @format date-time */
  freshestAt?: string;
  groupId?: string;
  /** @format double */
  hashrateTh?: number;
  /** @format int64 */
  holdConfirmedOwners?: number;
  /** @format int64 */
  holdReadyOwners?: number;
  /** Current interlocks that prevent a staged plan from becoming active. */
  holdReasons?: string[];
  miningAvailable?: boolean;
  partialCoverage?: boolean;
  /** @format int64 */
  pendingOwnerBoards?: number;
  planPhase?: string;
  /** @format int64 */
  planVersion?: string;
  /** Fresh firmware health with rails, clocks, or significant draw while held. */
  poweredWhileHeld?: V2MiningGroupPoweredBoard[];
  /** @format int64 */
  presentBoards?: number;
  /** @format int64 */
  reportingBoards?: number;
  /** @format double */
  requestedBoardWatts?: number;
  /** @format double */
  stagedBoardWatts?: number;
  /** @format int64 */
  stagedBoards?: number;
}

/**
 *  - MINING_MEASUREMENT_SCOPE_LOCAL_SNAPSHOTS: The fallback roster is process-local and cannot establish HA-wide coverage.
 * @default "MINING_MEASUREMENT_SCOPE_UNSPECIFIED"
 */
export enum V2MiningMeasurementScope {
  MINING_MEASUREMENT_SCOPE_UNSPECIFIED = "MINING_MEASUREMENT_SCOPE_UNSPECIFIED",
  MINING_MEASUREMENT_SCOPE_SHARED_ASSIGNMENTS = "MINING_MEASUREMENT_SCOPE_SHARED_ASSIGNMENTS",
  MINING_MEASUREMENT_SCOPE_LOCAL_SNAPSHOTS = "MINING_MEASUREMENT_SCOPE_LOCAL_SNAPSHOTS",
}

/** @default "MINING_MEASUREMENT_SOURCE_UNSPECIFIED" */
export enum V2MiningMeasurementSource {
  MINING_MEASUREMENT_SOURCE_UNSPECIFIED = "MINING_MEASUREMENT_SOURCE_UNSPECIFIED",
  MINING_MEASUREMENT_SOURCE_PERSISTED_BOARD_HEALTH = "MINING_MEASUREMENT_SOURCE_PERSISTED_BOARD_HEALTH",
}

/**
 * MiningOperatingState identifies the device-level mining outcome. This is
 * intentionally separate from the per-hashboard MiningState state machine.
 * @default "MINING_OPERATING_STATE_UNSPECIFIED"
 */
export enum V2MiningOperatingState {
  MINING_OPERATING_STATE_UNSPECIFIED = "MINING_OPERATING_STATE_UNSPECIFIED",
  MINING_OPERATING_STATE_UNINITIALIZED = "MINING_OPERATING_STATE_UNINITIALIZED",
  MINING_OPERATING_STATE_POWERING_ON = "MINING_OPERATING_STATE_POWERING_ON",
  MINING_OPERATING_STATE_MINING = "MINING_OPERATING_STATE_MINING",
  MINING_OPERATING_STATE_DEGRADED_MINING = "MINING_OPERATING_STATE_DEGRADED_MINING",
  MINING_OPERATING_STATE_POWERING_OFF = "MINING_OPERATING_STATE_POWERING_OFF",
  MINING_OPERATING_STATE_STOPPED = "MINING_OPERATING_STATE_STOPPED",
  MINING_OPERATING_STATE_NO_POOLS = "MINING_OPERATING_STATE_NO_POOLS",
  MINING_OPERATING_STATE_ERROR = "MINING_OPERATING_STATE_ERROR",
}

/**
 * MiningState represents the operational state of a hashboard.
 * @default "MINING_STATE_UNSPECIFIED"
 */
export enum V2MiningState {
  MINING_STATE_UNSPECIFIED = "MINING_STATE_UNSPECIFIED",
  MINING_STATE_DISCONNECTED = "MINING_STATE_DISCONNECTED",
  MINING_STATE_IDENTIFYING = "MINING_STATE_IDENTIFYING",
  MINING_STATE_CONFIGURING = "MINING_STATE_CONFIGURING",
  MINING_STATE_MINING = "MINING_STATE_MINING",
  MINING_STATE_ERROR = "MINING_STATE_ERROR",
  MINING_STATE_IDLE = "MINING_STATE_IDLE",
  MINING_STATE_INIT = "MINING_STATE_INIT",
  MINING_STATE_STARTUP = "MINING_STATE_STARTUP",
  MINING_STATE_READY = "MINING_STATE_READY",
  MINING_STATE_FLASHING = "MINING_STATE_FLASHING",
}

/** Module represents a module within a tank. */
export interface V2Module {
  /** @format int32 */
  id?: number;
  slots?: V2Slot[];
}

/**
 * ModuleDistribution summarizes current physical modules in known tanks.
 * Membership comes from shared locations and commissioned switch-port slots.
 * A configured module grid adds empty modules within those tanks; it does not
 * invent tanks absent from both sources. Unmapped boards are disclosed separately.
 */
export interface V2ModuleDistribution {
  /** @format date-time */
  evaluatedAt?: string;
  /**
   * Sources are the shared physical location catalog and current persisted
   * board health, independent of which HA peer owns the boards.
   * @format int64
   */
  expectedModules?: number;
  /** Hashboard output power is summed only for fully reporting modules. */
  hashboardPowerW?: V2ModuleDistributionStatistic;
  /** Each module is the mean of its boards' mean finite sensor temperatures. */
  hashboardTemperatureC?: V2ModuleDistributionStatistic;
  /** @format int64 */
  maxAgeSeconds?: number;
  /** PSU input power is counted once per module using its newest fresh sample. */
  psuInputPowerW?: V2ModuleDistributionStatistic;
  /** @format int64 */
  unmappedBoards?: number;
}

/**
 * ModuleDistributionStatistic describes equally weighted reporting modules.
 * Missing modules never contribute synthetic zeros. With partial coverage,
 * min/max/avg describe reporting modules only and complete is false. An empty
 * reporting set has absent numbers, including when the expected set is empty.
 */
export interface V2ModuleDistributionStatistic {
  /** @format double */
  avg?: number;
  /**
   * Also requires no unresolved board locations. This covers known topology;
   * unknown tanks cannot be inferred from telemetry.
   */
  complete?: boolean;
  /** @format double */
  max?: number;
  /** @format double */
  min?: number;
  /** @format date-time */
  newestObservedAt?: string;
  /** @format date-time */
  oldestObservedAt?: string;
  /** @format int64 */
  reportingModules?: number;
}

/** ModuleLayout describes the physical module grid inside a tank. */
export interface V2ModuleLayout {
  /** @format int64 */
  columns?: number;
  /** ModuleNumberingOrder describes how sequential module IDs fill a grid. */
  numbering?: V2ModuleNumberingOrder;
  /** @format int64 */
  rows?: number;
}

/**
 * ModuleNumberingOrder describes how sequential module IDs fill a grid.
 * @default "MODULE_NUMBERING_ORDER_UNSPECIFIED"
 */
export enum V2ModuleNumberingOrder {
  MODULE_NUMBERING_ORDER_UNSPECIFIED = "MODULE_NUMBERING_ORDER_UNSPECIFIED",
  MODULE_NUMBERING_ORDER_ROW_MAJOR = "MODULE_NUMBERING_ORDER_ROW_MAJOR",
  MODULE_NUMBERING_ORDER_COLUMN_MAJOR = "MODULE_NUMBERING_ORDER_COLUMN_MAJOR",
}

/** ModuleSelector selects a container module by tank and module number. */
export interface V2ModuleSelector {
  /** @format int32 */
  module?: number;
  /** @format int32 */
  tank?: number;
}

/** @default "MODULE_VOLTAGE_READINESS_UNSPECIFIED" */
export enum V2ModuleVoltageReadiness {
  MODULE_VOLTAGE_READINESS_UNSPECIFIED = "MODULE_VOLTAGE_READINESS_UNSPECIFIED",
  MODULE_VOLTAGE_READINESS_READY = "MODULE_VOLTAGE_READINESS_READY",
  MODULE_VOLTAGE_READINESS_LIMITS_UNAVAILABLE = "MODULE_VOLTAGE_READINESS_LIMITS_UNAVAILABLE",
  MODULE_VOLTAGE_READINESS_NO_MEMBERS = "MODULE_VOLTAGE_READINESS_NO_MEMBERS",
  MODULE_VOLTAGE_READINESS_TARGET_OUTSIDE_LIMITS = "MODULE_VOLTAGE_READINESS_TARGET_OUTSIDE_LIMITS",
  MODULE_VOLTAGE_READINESS_NO_COMMON_RANGE = "MODULE_VOLTAGE_READINESS_NO_COMMON_RANGE",
}

/**
 * ModuleVoltageSetting is one durable module voltage setting. An absent
 * fixed_millivolts field means firmware automatic voltage tuning.
 */
export interface V2ModuleVoltageSetting {
  /** @format int32 */
  boardCount?: number;
  /** @format int64 */
  fixedMillivolts?: number;
  moduleId?: string;
  readiness?: V2ModuleVoltageReadiness;
  readinessMessage?: string;
  /** @format int64 */
  supportedMaximumMillivolts?: number;
  /** @format int64 */
  supportedMinimumMillivolts?: number;
  /** @format date-time */
  updatedAt?: string;
  updatedBy?: string;
}

export type V2MuteContainerAlarmRequest = object;

export type V2MuteContainerAlarmResponse = object;

/** NetworkConfig contains network interface configuration. */
export interface V2NetworkConfig {
  dnsPrimary?: string;
  dnsSecondary?: string;
  gateway?: string;
  hostname?: string;
  ipAddress?: string;
  /**
   * mac_address is observation-only and absent when the interface identity
   * cannot be reported. SetNetworkConfig ignores this field and returns the
   * independently observed value in its response when available.
   */
  readonly macAddress?: string;
  /** NetworkMode represents the network interface configuration mode. */
  mode?: V2NetworkMode;
  subnetMask?: string;
}

/**
 * NetworkMode represents the network interface configuration mode.
 * @default "NETWORK_MODE_UNSPECIFIED"
 */
export enum V2NetworkMode {
  NETWORK_MODE_UNSPECIFIED = "NETWORK_MODE_UNSPECIFIED",
  NETWORK_MODE_DHCP = "NETWORK_MODE_DHCP",
  NETWORK_MODE_STATIC = "NETWORK_MODE_STATIC",
}

/** OSBuildInfo contains immutable operating-system image identity. */
export interface V2OSBuildInfo {
  /** @format date-time */
  builtAt?: string;
  gitHash?: string;
  machine?: string;
  name?: string;
  /** OSBuildVariant identifies the operating-system image variant. */
  variant?: V2OSBuildVariant;
  version?: string;
}

/**
 * OSBuildVariant identifies the operating-system image variant.
 * @default "OS_BUILD_VARIANT_UNSPECIFIED"
 */
export enum V2OSBuildVariant {
  OS_BUILD_VARIANT_UNSPECIFIED = "OS_BUILD_VARIANT_UNSPECIFIED",
  OS_BUILD_VARIANT_RELEASE = "OS_BUILD_VARIANT_RELEASE",
  OS_BUILD_VARIANT_MANUFACTURING = "OS_BUILD_VARIANT_MANUFACTURING",
  OS_BUILD_VARIANT_DEVELOPMENT = "OS_BUILD_VARIANT_DEVELOPMENT",
  OS_BUILD_VARIANT_UNKNOWN = "OS_BUILD_VARIANT_UNKNOWN",
}

export interface V2OperationalPolicyCapabilities {
  mappedFeedsAvailable?: boolean;
  powerControllerActive?: boolean;
  recoveryActive?: boolean;
  /** True when this Frigate evaluates container thermal alarms. */
  thermalAlarmsActive?: boolean;
}

export interface V2OperationalPolicyState {
  capabilities?: V2OperationalPolicyCapabilities;
  effective?: V2EffectiveOperationalPolicy;
  fingerprint?: string;
  /** @format uint64 */
  generation?: string;
  managed?: V2ManagedOperationalPolicy;
  /**
   * MQTTSourceConnectionConfig reports nonsecret MQTT source settings. The
   * password value is never returned through this API.
   */
  mqttSourceConnection?: V2MQTTSourceConnectionConfig;
  /** @format uint64 */
  profileRevision?: string;
  /** @format date-time */
  updatedAt?: string;
  updatedBy?: string;
}

/**
 * PLLAlgorithm selects firmware PLL tuning used with a power-target point.
 * @default "PLL_ALGORITHM_UNSPECIFIED"
 */
export enum V2PLLAlgorithm {
  PLL_ALGORITHM_UNSPECIFIED = "PLL_ALGORITHM_UNSPECIFIED",
  PLL_ALGORITHM_NONE = "PLL_ALGORITHM_NONE",
  PLL_ALGORITHM_VOLTAGE_IMBALANCE = "PLL_ALGORITHM_VOLTAGE_IMBALANCE",
  PLL_ALGORITHM_TEMPERATURE_GRADIENT_WITH_VOLTAGE = "PLL_ALGORITHM_TEMPERATURE_GRADIENT_WITH_VOLTAGE",
  PLL_ALGORITHM_FMARGIN = "PLL_ALGORITHM_FMARGIN",
  PLL_ALGORITHM_DEBUG_FUZZING = "PLL_ALGORITHM_DEBUG_FUZZING",
  PLL_ALGORITHM_FMARGIN_THERMAL_THROTTLE = "PLL_ALGORITHM_FMARGIN_THERMAL_THROTTLE",
  PLL_ALGORITHM_FMARGIN_DELTA_THERMAL_THROTTLE = "PLL_ALGORITHM_FMARGIN_DELTA_THERMAL_THROTTLE",
}

/** PSUFirmwareBoardStatus is one board's firmware-reported PSU update state. */
export interface V2PSUFirmwareBoardStatus {
  /** capsule_id is the lowercase hex SHA-256 of the capsule the phase refers to. */
  capsuleId?: string;
  /** PSUFirmwareComponentRole identifies one PSU controller. */
  component?: V2PSUFirmwareComponentRole;
  /** @format int64 */
  componentOffset?: number;
  currentAppRevision?: string;
  currentBootRevision?: string;
  /** PSUFirmwareError mirrors the board firmware's PSU update error. */
  error?: V2PSUFirmwareError;
  macAddress?: string;
  /** PSUFirmwarePhase mirrors the board firmware's PSU update phase. */
  phase?: V2PSUFirmwarePhase;
  /** @format int64 */
  programmedBytes?: number;
  /** psu_leader reports whether the board is its module's valid PSU leader. */
  psuLeader?: boolean;
  recoveryRequired?: boolean;
  /** @format int64 */
  stagedBytes?: number;
  /** @format int64 */
  totalSizeBytes?: number;
  /** @format int64 */
  vendorStatus?: number;
}

/** PSUFirmwareCapsule describes one locally verified, cached capsule. */
export interface V2PSUFirmwareCapsule {
  /** capsule_id is the lowercase hex SHA-256 of the complete sealed capsule. */
  capsuleId?: string;
  components?: V2PSUFirmwareComponent[];
  /** file_name is the client-supplied display name. */
  fileName?: string;
  manufacturerId?: string;
  manufacturerModel?: string;
  manufacturerRevision?: string;
  /**
   * signer_subject is the embedded leaf certificate subject. Board firmware
   * decides whether that signer is trusted.
   */
  signerSubject?: string;
  /** @format int64 */
  sizeBytes?: string;
  /** @format date-time */
  uploadedAt?: string;
  /** vendor is the PSU payload vendor, for example "BOCO". */
  vendor?: string;
}

export interface V2PSUFirmwareCapsuleUploadMetadata {
  /** sha256 is the optional expected lowercase hex SHA-256 of the capsule. */
  sha256?: string;
  /** file_name is a safe single-component display name. */
  fileName?: string;
  /**
   * size_bytes is the exact capsule size, at most 512 KiB.
   * @format int64
   */
  sizeBytes?: string;
}

/** PSUFirmwareComponent describes one vendor image in a capsule. */
export interface V2PSUFirmwareComponent {
  /** sha256 is the lowercase hex digest of the complete vendor image. */
  sha256?: string;
  /** @format int64 */
  imageSizeBytes?: number;
  /** PSUFirmwareComponentRole identifies one PSU controller. */
  role?: V2PSUFirmwareComponentRole;
  /** @format int64 */
  targetId?: number;
}

/**
 * PSUFirmwareComponentRole identifies one PSU controller.
 * @default "PSU_FIRMWARE_COMPONENT_ROLE_UNSPECIFIED"
 */
export enum V2PSUFirmwareComponentRole {
  PSU_FIRMWARE_COMPONENT_ROLE_UNSPECIFIED = "PSU_FIRMWARE_COMPONENT_ROLE_UNSPECIFIED",
  PSU_FIRMWARE_COMPONENT_ROLE_PRIMARY1 = "PSU_FIRMWARE_COMPONENT_ROLE_PRIMARY1",
  PSU_FIRMWARE_COMPONENT_ROLE_PRIMARY2 = "PSU_FIRMWARE_COMPONENT_ROLE_PRIMARY2",
  PSU_FIRMWARE_COMPONENT_ROLE_SECONDARY = "PSU_FIRMWARE_COMPONENT_ROLE_SECONDARY",
}

/**
 * PSUFirmwareError mirrors the board firmware's PSU update error.
 * @default "PSU_FIRMWARE_ERROR_UNSPECIFIED"
 */
export enum V2PSUFirmwareError {
  PSU_FIRMWARE_ERROR_UNSPECIFIED = "PSU_FIRMWARE_ERROR_UNSPECIFIED",
  PSU_FIRMWARE_ERROR_NONE = "PSU_FIRMWARE_ERROR_NONE",
  PSU_FIRMWARE_ERROR_NOT_LEADER = "PSU_FIRMWARE_ERROR_NOT_LEADER",
  PSU_FIRMWARE_ERROR_STORAGE = "PSU_FIRMWARE_ERROR_STORAGE",
  PSU_FIRMWARE_ERROR_CAPSULE_FORMAT = "PSU_FIRMWARE_ERROR_CAPSULE_FORMAT",
  PSU_FIRMWARE_ERROR_CAPSULE_HASH = "PSU_FIRMWARE_ERROR_CAPSULE_HASH",
  PSU_FIRMWARE_ERROR_CAPSULE_SIGNATURE = "PSU_FIRMWARE_ERROR_CAPSULE_SIGNATURE",
  PSU_FIRMWARE_ERROR_CAPSULE_TRUST = "PSU_FIRMWARE_ERROR_CAPSULE_TRUST",
  PSU_FIRMWARE_ERROR_TARGET_MISMATCH = "PSU_FIRMWARE_ERROR_TARGET_MISMATCH",
  PSU_FIRMWARE_ERROR_CLUSTER_INCOMPATIBLE = "PSU_FIRMWARE_ERROR_CLUSTER_INCOMPATIBLE",
  PSU_FIRMWARE_ERROR_BARRIER_TIMEOUT = "PSU_FIRMWARE_ERROR_BARRIER_TIMEOUT",
  PSU_FIRMWARE_ERROR_I2C = "PSU_FIRMWARE_ERROR_I2C",
  PSU_FIRMWARE_ERROR_BOOTLOADER_STATE = "PSU_FIRMWARE_ERROR_BOOTLOADER_STATE",
  PSU_FIRMWARE_ERROR_VENDOR_STATUS = "PSU_FIRMWARE_ERROR_VENDOR_STATUS",
}

/**
 * PSUFirmwarePhase mirrors the board firmware's PSU update phase.
 * @default "PSU_FIRMWARE_PHASE_UNSPECIFIED"
 */
export enum V2PSUFirmwarePhase {
  PSU_FIRMWARE_PHASE_UNSPECIFIED = "PSU_FIRMWARE_PHASE_UNSPECIFIED",
  PSU_FIRMWARE_PHASE_EMPTY = "PSU_FIRMWARE_PHASE_EMPTY",
  PSU_FIRMWARE_PHASE_STAGING = "PSU_FIRMWARE_PHASE_STAGING",
  PSU_FIRMWARE_PHASE_VALIDATING = "PSU_FIRMWARE_PHASE_VALIDATING",
  PSU_FIRMWARE_PHASE_READY = "PSU_FIRMWARE_PHASE_READY",
  PSU_FIRMWARE_PHASE_FENCING = "PSU_FIRMWARE_PHASE_FENCING",
  PSU_FIRMWARE_PHASE_ENTERING_BOOTLOADER = "PSU_FIRMWARE_PHASE_ENTERING_BOOTLOADER",
  PSU_FIRMWARE_PHASE_PROGRAMMING = "PSU_FIRMWARE_PHASE_PROGRAMMING",
  PSU_FIRMWARE_PHASE_EXITING_BOOTLOADER = "PSU_FIRMWARE_PHASE_EXITING_BOOTLOADER",
  PSU_FIRMWARE_PHASE_REPROBING = "PSU_FIRMWARE_PHASE_REPROBING",
  PSU_FIRMWARE_PHASE_VERIFYING = "PSU_FIRMWARE_PHASE_VERIFYING",
  PSU_FIRMWARE_PHASE_SUCCEEDED = "PSU_FIRMWARE_PHASE_SUCCEEDED",
  PSU_FIRMWARE_PHASE_FAILED = "PSU_FIRMWARE_PHASE_FAILED",
  PSU_FIRMWARE_PHASE_RECOVERY_REQUIRED = "PSU_FIRMWARE_PHASE_RECOVERY_REQUIRED",
}

/** PSUFirmwareUpdate is one module update job. */
export interface V2PSUFirmwareUpdate {
  /**
   * apply_requested reports whether the apply command may have reached the
   * leader. After that point the job cannot be cancelled.
   */
  applyRequested?: boolean;
  capsuleId?: string;
  /** @format date-time */
  completedAt?: string;
  /** @format date-time */
  createdAt?: string;
  error?: string;
  force?: boolean;
  leaderMacAddress?: string;
  /** leader_status is the most recent firmware status read from the leader. */
  leaderStatus?: V2PSUFirmwareBoardStatus;
  members?: V2PSUFirmwareUpdateMember[];
  moduleId?: string;
  recover?: boolean;
  /** server_id is the Frigate node that owns this job. */
  serverId?: string;
  /** @format date-time */
  startedAt?: string;
  /**
   * PSUFirmwareUpdateState is the Frigate job lifecycle.
   *
   *  - PSU_FIRMWARE_UPDATE_STATE_QUEUED: QUEUED waits for a module update slot.
   *  - PSU_FIRMWARE_UPDATE_STATE_PREPARING: PREPARING stops member mining and resolves the PSU leader.
   *  - PSU_FIRMWARE_UPDATE_STATE_STAGING: STAGING transfers the capsule to the leader.
   *  - PSU_FIRMWARE_UPDATE_STATE_VALIDATING: VALIDATING waits for the leader to verify the staged capsule.
   *  - PSU_FIRMWARE_UPDATE_STATE_APPLYING: APPLYING waits for board firmware to program and verify the PSU.
   *  - PSU_FIRMWARE_UPDATE_STATE_RESTORING: RESTORING restores mining on members that were mining before the job.
   */
  state?: V2PSUFirmwareUpdateState;
  taskId?: string;
}

/** PSUFirmwareUpdateMember is one reserved module member. */
export interface V2PSUFirmwareUpdateMember {
  macAddress?: string;
  /** mining_before reports whether mining intent was set when the job started. */
  miningBefore?: boolean;
  /**
   * reserved reports whether Frigate reserved the member. Disconnected or
   * unmanaged members are not reserved; board firmware still fences them.
   */
  reserved?: boolean;
  /** restore_error is set when mining could not be restored. */
  restoreError?: string;
}

/**
 * PSUFirmwareUpdateState is the Frigate job lifecycle.
 *
 *  - PSU_FIRMWARE_UPDATE_STATE_QUEUED: QUEUED waits for a module update slot.
 *  - PSU_FIRMWARE_UPDATE_STATE_PREPARING: PREPARING stops member mining and resolves the PSU leader.
 *  - PSU_FIRMWARE_UPDATE_STATE_STAGING: STAGING transfers the capsule to the leader.
 *  - PSU_FIRMWARE_UPDATE_STATE_VALIDATING: VALIDATING waits for the leader to verify the staged capsule.
 *  - PSU_FIRMWARE_UPDATE_STATE_APPLYING: APPLYING waits for board firmware to program and verify the PSU.
 *  - PSU_FIRMWARE_UPDATE_STATE_RESTORING: RESTORING restores mining on members that were mining before the job.
 * @default "PSU_FIRMWARE_UPDATE_STATE_UNSPECIFIED"
 */
export enum V2PSUFirmwareUpdateState {
  PSU_FIRMWARE_UPDATE_STATE_UNSPECIFIED = "PSU_FIRMWARE_UPDATE_STATE_UNSPECIFIED",
  PSU_FIRMWARE_UPDATE_STATE_QUEUED = "PSU_FIRMWARE_UPDATE_STATE_QUEUED",
  PSU_FIRMWARE_UPDATE_STATE_PREPARING = "PSU_FIRMWARE_UPDATE_STATE_PREPARING",
  PSU_FIRMWARE_UPDATE_STATE_STAGING = "PSU_FIRMWARE_UPDATE_STATE_STAGING",
  PSU_FIRMWARE_UPDATE_STATE_VALIDATING = "PSU_FIRMWARE_UPDATE_STATE_VALIDATING",
  PSU_FIRMWARE_UPDATE_STATE_APPLYING = "PSU_FIRMWARE_UPDATE_STATE_APPLYING",
  PSU_FIRMWARE_UPDATE_STATE_RESTORING = "PSU_FIRMWARE_UPDATE_STATE_RESTORING",
  PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED = "PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED",
  PSU_FIRMWARE_UPDATE_STATE_FAILED = "PSU_FIRMWARE_UPDATE_STATE_FAILED",
  PSU_FIRMWARE_UPDATE_STATE_CANCELLED = "PSU_FIRMWARE_UPDATE_STATE_CANCELLED",
}

/** PageResponse contains pagination metadata. */
export interface V2PageResponse {
  nextPageToken?: string;
  /** @format int32 */
  totalCount?: number;
}

/**
 * PerformanceMode selects whether the miner prioritizes maximum hashrate or
 * efficiency within its power target. It is distinct from TuningAlgorithm.
 * @default "PERFORMANCE_MODE_UNSPECIFIED"
 */
export enum V2PerformanceMode {
  PERFORMANCE_MODE_UNSPECIFIED = "PERFORMANCE_MODE_UNSPECIFIED",
  PERFORMANCE_MODE_MAXIMUM_HASHRATE = "PERFORMANCE_MODE_MAXIMUM_HASHRATE",
  PERFORMANCE_MODE_EFFICIENCY = "PERFORMANCE_MODE_EFFICIENCY",
}

/** PerformanceSweep configures optional target-power sweep steps. */
export interface V2PerformanceSweep {
  steps?: V2PerformanceSweepStep[];
}

/** PerformanceSweepStep is one target-power step in a sweep. */
export interface V2PerformanceSweepStep {
  /** @format double */
  boardPowerTargetWatts?: number;
  duration?: string;
}

/**
 * Phase represents an electrical phase.
 * @default "PHASE_UNSPECIFIED"
 */
export enum V2Phase {
  PHASE_UNSPECIFIED = "PHASE_UNSPECIFIED",
  PHASE_A = "PHASE_A",
  PHASE_B = "PHASE_B",
  PHASE_C = "PHASE_C",
}

/** PhaseAggregate contains aggregate power draw for a single electrical phase. */
export interface V2PhaseAggregate {
  /** @format double */
  currentA?: number;
  /** Phase represents an electrical phase. */
  phase?: V2Phase;
  /** @format double */
  powerW?: number;
  /** @format double */
  voltageV?: number;
}

export interface V2PlanFeedMappingRunResponse {
  /** FeedMappingRun is a snapshot of a feed mapping run. */
  plan?: V2FeedMappingRun;
}

/** PlantAlarm identifies one currently active backend alarm. */
export interface V2PlantAlarm {
  componentId?: string;
  id?: string;
  name?: string;
  scope?: string;
  /** Severity represents the severity level of an alert. */
  severity?: V2Severity;
}

export interface V2PlantCoolingFan {
  alarmActive?: boolean;
  componentId?: string;
  /** PlantCoolingFan contains one cooling fan's latest readings. */
  direction?: V2PlantCoolingFanDirection;
  /** @format double */
  powerW?: number;
  /** @format double */
  speedPercent?: number;
}

/**
 * PlantCoolingFan contains one cooling fan's latest readings.
 * @default "PLANT_COOLING_FAN_DIRECTION_UNSPECIFIED"
 */
export enum V2PlantCoolingFanDirection {
  PLANT_COOLING_FAN_DIRECTION_UNSPECIFIED = "PLANT_COOLING_FAN_DIRECTION_UNSPECIFIED",
  PLANT_COOLING_FAN_DIRECTION_FORWARD = "PLANT_COOLING_FAN_DIRECTION_FORWARD",
  PLANT_COOLING_FAN_DIRECTION_REVERSE = "PLANT_COOLING_FAN_DIRECTION_REVERSE",
  PLANT_COOLING_FAN_DIRECTION_UNKNOWN = "PLANT_COOLING_FAN_DIRECTION_UNKNOWN",
}

/**
 * PlantFault contains one component's current numeric fault code and optional
 * V0821 error bitmap. The legacy code field remains the fault code so older
 * consumers keep one unique record per component and safely ignore the
 * additive bitmap.
 */
export interface V2PlantFault {
  /** @format int64 */
  code?: number;
  componentId?: string;
  /**
   * Absence means the V0821 error-bitmap telemetry is unavailable. A present
   * zero means the controller supports it and currently reports no error bits.
   * @format int64
   */
  errorBitmap?: number;
  /**
   * Deprecated: error_code was introduced before the V0821 value was
   * identified as a bitmap. New producers mirror error_bitmap here during the
   * rolling transition so older generated clients remain source/JSON/wire
   * compatible. New consumers accept this field only as a bitmap fallback.
   * @format int64
   */
  errorCode?: number;
}

/** PlantHealth contains normalized optional container-plant telemetry. */
export interface V2PlantHealth {
  /** @format double */
  activeEnergyKwh?: number;
  alarmActive?: boolean;
  /**
   * Deprecated compatibility alias for inside_ambient_humidity_percent.
   * @format double
   */
  ambientHumidityPercent?: number;
  /**
   * Deprecated compatibility alias for outside_ambient_temperature_c.
   * @format double
   */
  ambientTemperatureC?: number;
  /** @format double */
  apparentEnergyKvah?: number;
  /** @format double */
  apparentPowerVa?: number;
  /**
   * BuzzerActive is the controller's direct audible-alarm output readback.
   * It is absent when the selected PLC transport does not expose that state.
   */
  buzzerActive?: boolean;
  coolantPumpActive?: boolean;
  /** @format double */
  coolantPumpSpeedHz?: number;
  criticalAlarmActive?: boolean;
  dryCoolerFanReverse?: V2DryCoolerFanReverseStatus;
  /** @format double */
  dryCoolerInletTemperatureC?: number;
  /** @format double */
  facilityPowerW?: number;
  /** @format double */
  filterPressureDiffPa?: number;
  /** @format double */
  flowRateLpm?: number;
  heatRejectionActive?: boolean;
  /** @format double */
  heatRejectionInletPressurePa?: number;
  /** @format double */
  heatRejectionOutletPressurePa?: number;
  /** @format double */
  heatRejectionPowerW?: number;
  /** @format double */
  heatRejectionPressureDiffPa?: number;
  /** @format double */
  inletTemperatureC?: number;
  /** @format double */
  insideAmbientHumidityPercent?: number;
  /** @format double */
  insideAmbientTemperatureC?: number;
  /** PlantState represents a backend-reported container-plant operating state. */
  operatingState?: V2PlantState;
  /** @format double */
  outletATemperatureC?: number;
  /** @format double */
  outletBTemperatureC?: number;
  /** @format double */
  outletTemperatureC?: number;
  /** @format double */
  outsideAmbientHumidityPercent?: number;
  /** @format double */
  outsideAmbientTemperatureC?: number;
  /** @format double */
  powerFactor?: number;
  powered?: boolean;
  /** @format double */
  pue?: number;
  /** @format double */
  pumpInletPressurePa?: number;
  /** @format double */
  pumpOutletPressurePa?: number;
  /** @format double */
  pumpPressureDiffPa?: number;
  /** @format double */
  reactiveEnergyKvarh?: number;
  /** @format double */
  reactivePowerVar?: number;
  /** @format double */
  tankALiquidLevelMm?: number;
  /** @format double */
  tankBLiquidLevelMm?: number;
  /** @format double */
  totalPowerW?: number;
}

/** PlantOperatingParameter is one read-only raw PLC operating threshold. */
export interface V2PlantOperatingParameter {
  boundary?: string;
  code?: string;
  unit?: string;
  /** @format double */
  value?: number;
}

/** PlantPowerDistribution contains one distribution unit's latest readings. */
export interface V2PlantPowerDistribution {
  /** @format double */
  activeEnergyKwh?: number;
  /** @format int32 */
  activeFeedCount?: number;
  /** @format double */
  activePowerW?: number;
  componentId?: string;
  /** @format double */
  currentAA?: number;
  /** @format double */
  currentBA?: number;
  /** @format double */
  currentCA?: number;
  /** @format double */
  voltageAbV?: number;
  /** @format double */
  voltageBcV?: number;
  /** @format double */
  voltageCaV?: number;
}

/**
 * PlantState represents a backend-reported container-plant operating state.
 * @default "PLANT_STATE_UNSPECIFIED"
 */
export enum V2PlantState {
  PLANT_STATE_UNSPECIFIED = "PLANT_STATE_UNSPECIFIED",
  PLANT_STATE_RUNNING = "PLANT_STATE_RUNNING",
  PLANT_STATE_STANDBY = "PLANT_STATE_STANDBY",
  PLANT_STATE_SLEEPING = "PLANT_STATE_SLEEPING",
}

/** @default "PLANT_WRITE_CONFIRMATION_SOURCE_UNSPECIFIED" */
export enum V2PlantWriteConfirmationSource {
  PLANT_WRITE_CONFIRMATION_SOURCE_UNSPECIFIED = "PLANT_WRITE_CONFIRMATION_SOURCE_UNSPECIFIED",
  PLANT_WRITE_CONFIRMATION_SOURCE_CONTROLLER_REGISTER = "PLANT_WRITE_CONFIRMATION_SOURCE_CONTROLLER_REGISTER",
}

/**
 *  - PLANT_WRITE_CONFIRMATION_STATE_UNAVAILABLE: The write succeeded, but authoritative observation is unavailable.
 * @default "PLANT_WRITE_CONFIRMATION_STATE_UNSPECIFIED"
 */
export enum V2PlantWriteConfirmationState {
  PLANT_WRITE_CONFIRMATION_STATE_UNSPECIFIED = "PLANT_WRITE_CONFIRMATION_STATE_UNSPECIFIED",
  PLANT_WRITE_CONFIRMATION_STATE_CONFIRMED = "PLANT_WRITE_CONFIRMATION_STATE_CONFIRMED",
  PLANT_WRITE_CONFIRMATION_STATE_MISMATCH = "PLANT_WRITE_CONFIRMATION_STATE_MISMATCH",
  PLANT_WRITE_CONFIRMATION_STATE_UNAVAILABLE = "PLANT_WRITE_CONFIRMATION_STATE_UNAVAILABLE",
}

/**
 * Pool represents a mining pool configuration.
 * The password field is write-only and never included in responses.
 */
export interface V2Pool {
  /** @format date-time */
  createdAt?: string;
  enabled?: boolean;
  /** @format int64 */
  id?: string;
  name?: string;
  /** @format int32 */
  priority?: number;
  /**
   * share_weight enables weighted mining for this pool. It is a relative,
   * positive weight; when absent the pool remains a priority failover target.
   * @format double
   */
  shareWeight?: number;
  url?: string;
  username?: string;
}

/**
 * PoolAllocationMode is container scoped. A future board assignment strategy
 * can extend this enum without changing the existing pool credential schema.
 * @default "POOL_ALLOCATION_MODE_UNSPECIFIED"
 */
export enum V2PoolAllocationMode {
  POOL_ALLOCATION_MODE_UNSPECIFIED = "POOL_ALLOCATION_MODE_UNSPECIFIED",
  POOL_ALLOCATION_MODE_PRIORITY = "POOL_ALLOCATION_MODE_PRIORITY",
  POOL_ALLOCATION_MODE_WEIGHTED = "POOL_ALLOCATION_MODE_WEIGHTED",
}

/**
 * PoolConnState mirrors the runtime connection state tracked by pkg/pools.
 *
 *  - POOL_CONN_STATE_IDLE: POOL_CONN_STATE_IDLE indicates the pool is available but not actively mining.
 *  - POOL_CONN_STATE_ACTIVE: POOL_CONN_STATE_ACTIVE indicates the pool is a current mining target.
 *  - POOL_CONN_STATE_DEAD: POOL_CONN_STATE_DEAD indicates the pool is unreachable.
 * @default "POOL_CONN_STATE_UNSPECIFIED"
 */
export enum V2PoolConnState {
  POOL_CONN_STATE_UNSPECIFIED = "POOL_CONN_STATE_UNSPECIFIED",
  POOL_CONN_STATE_IDLE = "POOL_CONN_STATE_IDLE",
  POOL_CONN_STATE_ACTIVE = "POOL_CONN_STATE_ACTIVE",
  POOL_CONN_STATE_DEAD = "POOL_CONN_STATE_DEAD",
}

/** PoolConnectionDiagnostics contains runtime connection diagnostics for one pool. */
export interface V2PoolConnectionDiagnostics {
  /**
   * configured_share_weight is zero when this pool is not weighted.
   * @format double
   */
  configuredShareWeight?: number;
  /**
   * PoolConnState mirrors the runtime connection state tracked by pkg/pools.
   *
   *  - POOL_CONN_STATE_IDLE: POOL_CONN_STATE_IDLE indicates the pool is available but not actively mining.
   *  - POOL_CONN_STATE_ACTIVE: POOL_CONN_STATE_ACTIVE indicates the pool is a current mining target.
   *  - POOL_CONN_STATE_DEAD: POOL_CONN_STATE_DEAD indicates the pool is unreachable.
   */
  connState?: V2PoolConnState;
  /** @format double */
  currentDifficulty?: number;
  /**
   * effective_share is this pool's normalized target work fraction once the
   * current session is ready. A ready failover-only pool reports 1 when every
   * weighted pool is unavailable. It is zero when inactive or not yet ready.
   * @format double
   */
  effectiveShare?: number;
  isActive?: boolean;
  /** is_ready is true after this connection session has delivered usable work. */
  isReady?: boolean;
  lastFailoverReason?: string;
  /** @format date-time */
  lastFailoverTime?: string;
  /** @format int64 */
  poolId?: string;
  poolName?: string;
  /** @format int64 */
  reconnectCount?: string;
  /**
   * seconds_since_last_notify is -1 when a notify was never received.
   * @format int64
   */
  secondsSinceLastNotify?: string;
  /**
   * session_id changes on every successful connection activation.
   * @format uint64
   */
  sessionId?: string;
}

export interface V2PoolConnectionSharingDiagnostic {
  authorized?: boolean;
  mode?: string;
  /** @format uint64 */
  physicalGeneration?: string;
  /** @format int32 */
  poolId?: number;
  profileFingerprint?: string;
  profileId?: string;
  profileRevision?: string;
  profileSource?: string;
  reason?: string;
  upstreamId?: string;
}

export interface V2PoolConnectionSharingProfile {
  cleanupNotice?: string;
  disabled?: boolean;
  endpoint?: string;
  evidenceRef?: string;
  fingerprint?: string;
  id?: string;
  /** @format int64 */
  maxAccounts?: number;
  /** @format double */
  maxHashrateTh?: number;
  /** @format double */
  maxShareIntervalSeconds?: number;
  /** @format double */
  minAccountHashrateTh?: number;
  profileKey?: string;
  qualificationDate?: string;
  quarantineFingerprint?: string;
  quarantineReason?: string;
  quarantined?: boolean;
  /** @format date-time */
  quarantinedAt?: string;
  reviewer?: string;
  revision?: string;
  revoked?: boolean;
  source?: string;
  workPolicy?: string;
}

export interface V2PoolConnectionSharingState {
  blocker?: string;
  configurationAllowed?: boolean;
  desiredEnabled?: boolean;
  effectiveEnabled?: boolean;
  /** @format int64 */
  implementationMaxAccounts?: number;
  /** @format int64 */
  logicalLeases?: number;
  /** @format date-time */
  observedAt?: string;
  observerServerId?: string;
  /** @format int64 */
  physicalConnections?: number;
  profiles?: V2PoolConnectionSharingProfile[];
  registryFingerprint?: string;
  /** @format int64 */
  revision?: string;
  supported?: boolean;
}

/**
 * PoolRecordedShareResult is the latest retained result for one configured
 * pool. The timestamp is recorded after the pool submission returns, not when
 * submission began. The difficulty is the assigned work target, not the
 * difficulty achieved by the submitted nonce. Rejected and failed submissions
 * are included. Missing optional values reflect legacy rows with no outcome or
 * target difficulty.
 */
export interface V2PoolRecordedShareResult {
  accepted?: boolean;
  /** @format int64 */
  assignedDifficulty?: string;
  /** @format int64 */
  poolId?: string;
  /**
   * reject_reason is empty for accepted shares or older rows without a reason.
   * "submit_error" identifies a transport/submit failure.
   */
  rejectReason?: string;
  /** @format date-time */
  resultAt?: string;
}

/** PowerFeed contains one container power feed and optional telemetry. */
export interface V2PowerFeed {
  active?: boolean;
  /** @format double */
  currentA?: number;
  /** @format double */
  currentAA?: number;
  /** @format double */
  currentBA?: number;
  /** @format double */
  currentCA?: number;
  faulted?: boolean;
  overridden?: boolean;
  /** Phase represents an electrical phase. */
  phase?: V2Phase;
  powerFeedId?: string;
  /** @format double */
  powerW?: number;
  /** PowerFeedState represents the state of a container power feed. */
  state?: V2PowerFeedState;
  /** @format double */
  voltageAbV?: number;
  /** @format double */
  voltageBcV?: number;
  /** @format double */
  voltageCaV?: number;
  /** @format double */
  voltageV?: number;
}

/**
 * PowerFeedState represents the state of a container power feed.
 * @default "POWER_FEED_STATE_UNSPECIFIED"
 */
export enum V2PowerFeedState {
  POWER_FEED_STATE_UNSPECIFIED = "POWER_FEED_STATE_UNSPECIFIED",
  POWER_FEED_STATE_ENABLED = "POWER_FEED_STATE_ENABLED",
  POWER_FEED_STATE_DISABLED = "POWER_FEED_STATE_DISABLED",
}

export interface V2PowerRecoveryCycle {
  /** @format date-time */
  createdAt?: string;
  id?: string;
  lastError?: string;
  outletIds?: string[];
  phase?: string;
  /** @format date-time */
  restoreAfter?: string;
  scope?: string;
}

/** PowerSupply represents an installed power supply unit. */
export interface V2PowerSupply {
  /**
   * bootloader_version is absent when the PSU cannot report it independently
   * of its application firmware version.
   */
  bootloaderVersion?: string;
  /** @format double */
  current?: number;
  /**
   * PowerSupplyFirmwareUpdateStatus contains update metadata rather than live
   * electrical telemetry.
   */
  firmwareUpdate?: V2PowerSupplyFirmwareUpdateStatus;
  firmwareVersion?: string;
  hardwareRevision?: string;
  id?: string;
  manufacturer?: string;
  model?: string;
  /** @format double */
  power?: number;
  serialNumber?: string;
  /**
   * slot is one-based when reported by the platform.
   * @format int64
   */
  slot?: number;
  /** PowerSupplyStatus represents the operational state of a PSU. */
  status?: V2PowerSupplyStatus;
  /** @format double */
  temperature?: number;
  vendor?: string;
  /** @format double */
  voltage?: number;
}

/**
 * PowerSupplyFirmwareUpdateState identifies one PSU update lifecycle.
 * @default "POWER_SUPPLY_FIRMWARE_UPDATE_STATE_UNSPECIFIED"
 */
export enum V2PowerSupplyFirmwareUpdateState {
  POWER_SUPPLY_FIRMWARE_UPDATE_STATE_UNSPECIFIED = "POWER_SUPPLY_FIRMWARE_UPDATE_STATE_UNSPECIFIED",
  POWER_SUPPLY_FIRMWARE_UPDATE_STATE_IDLE = "POWER_SUPPLY_FIRMWARE_UPDATE_STATE_IDLE",
  POWER_SUPPLY_FIRMWARE_UPDATE_STATE_UPLOADING = "POWER_SUPPLY_FIRMWARE_UPDATE_STATE_UPLOADING",
  POWER_SUPPLY_FIRMWARE_UPDATE_STATE_VERIFYING = "POWER_SUPPLY_FIRMWARE_UPDATE_STATE_VERIFYING",
  POWER_SUPPLY_FIRMWARE_UPDATE_STATE_COMPLETE = "POWER_SUPPLY_FIRMWARE_UPDATE_STATE_COMPLETE",
  POWER_SUPPLY_FIRMWARE_UPDATE_STATE_FAILED = "POWER_SUPPLY_FIRMWARE_UPDATE_STATE_FAILED",
}

/**
 * PowerSupplyFirmwareUpdateStatus contains update metadata rather than live
 * electrical telemetry.
 */
export interface V2PowerSupplyFirmwareUpdateStatus {
  availableVersion?: string;
  message?: string;
  /** @format double */
  progressPercent?: number;
  /** PowerSupplyFirmwareUpdateState identifies one PSU update lifecycle. */
  state?: V2PowerSupplyFirmwareUpdateState;
}

/**
 * PowerSupplyStatus represents the operational state of a PSU.
 * @default "POWER_SUPPLY_STATUS_UNSPECIFIED"
 */
export enum V2PowerSupplyStatus {
  POWER_SUPPLY_STATUS_UNSPECIFIED = "POWER_SUPPLY_STATUS_UNSPECIFIED",
  POWER_SUPPLY_STATUS_ONLINE = "POWER_SUPPLY_STATUS_ONLINE",
  POWER_SUPPLY_STATUS_OFFLINE = "POWER_SUPPLY_STATUS_OFFLINE",
  POWER_SUPPLY_STATUS_WARNING = "POWER_SUPPLY_STATUS_WARNING",
  POWER_SUPPLY_STATUS_ERROR = "POWER_SUPPLY_STATUS_ERROR",
}

/**
 * PowerSupplySummary contains PSU inventory identity without duplicating live
 * electrical telemetry.
 */
export interface V2PowerSupplySummary {
  /**
   * bootloader_version is absent when the PSU cannot report it independently
   * of its application firmware version.
   */
  bootloaderVersion?: string;
  /**
   * PowerSupplyFirmwareUpdateStatus contains update metadata rather than live
   * electrical telemetry.
   */
  firmwareUpdate?: V2PowerSupplyFirmwareUpdateStatus;
  firmwareVersion?: string;
  hardwareRevision?: string;
  id?: string;
  manufacturer?: string;
  model?: string;
  serialNumber?: string;
  /**
   * slot is one-based when reported by the platform.
   * @format int64
   */
  slot?: number;
  /** PowerSupplyStatus represents the operational state of a PSU. */
  status?: V2PowerSupplyStatus;
  vendor?: string;
}

/** PowerTargetRange defines requested board power-target sweep points in watts. */
export interface V2PowerTargetRange {
  /** @format double */
  endPowerTargetWatts?: number;
  /** @format double */
  startPowerTargetWatts?: number;
  /** @format double */
  stepPowerTargetWatts?: number;
}

export interface V2ProductionHistoryBucket {
  /** @format double */
  boardPowerAvgW?: number;
  /** @format double */
  boardPowerMaxW?: number;
  /** @format double */
  boardPowerMinW?: number;
  /** @format double */
  desiredPowerW?: number;
  /** @format double */
  efficiencyJTh?: number;
  /** @format uint64 */
  errorCount?: string;
  /** @format int64 */
  expectedBoards?: number;
  /** @format double */
  facilityPowerAvgW?: number;
  /** @format double */
  facilityPowerMaxW?: number;
  /** @format double */
  facilityPowerMinW?: number;
  /** @format double */
  hashrateAvgThs?: number;
  /** @format double */
  hashrateMaxThs?: number;
  /** @format double */
  hashrateMinThs?: number;
  /** @format uint64 */
  issueCount?: string;
  /** @format double */
  maximumBoardTemperatureC?: number;
  /** @format int64 */
  miningBoards?: number;
  /** @format double */
  miningSeconds?: number;
  /** @format double */
  observedSeconds?: number;
  /** @format int64 */
  onlineBoards?: number;
  /** @format double */
  pduPowerAvgW?: number;
  /** @format double */
  pduPowerMaxW?: number;
  /** @format double */
  pduPowerMinW?: number;
  /** @format double */
  pue?: number;
  /** @format int64 */
  reportingBoards?: number;
  /** @format double */
  reportingCoverage?: number;
  /** @format uint64 */
  sampleCount?: string;
  /** @format date-time */
  time?: string;
}

/** PsuElectricalHealth contains one complete power-supply electrical snapshot. */
export interface V2PsuElectricalHealth {
  /** @format double */
  inputCurrent?: number;
  /** @format double */
  inputPower?: number;
  /** @format double */
  inputVoltage?: number;
  /** @format double */
  outputCurrent?: number;
  /** @format double */
  outputPower?: number;
  /** @format double */
  outputVoltage?: number;
  /** @format int64 */
  sampleAgeMs?: number;
}

/**
 * PsuIdentity is manufacturer metadata for the shared power supply
 * identified by Hashboard.module_id. It is absent until compatible firmware
 * has reported and Frigate has durably retained the details.
 */
export interface V2PsuIdentity {
  /**
   * firmware_version is the latest reported PSU firmware; empty means unavailable.
   * It can change without changing the module identity.
   */
  firmwareVersion?: string;
  model?: string;
  psuType?: string;
  revision?: string;
  /** serial is the PSU manufacturer serial, not the hashboard PCBA serial. */
  serial?: string;
  vendor?: string;
}

/** PsuThermalHealth contains the power supply's thermal telemetry. */
export interface V2PsuThermalHealth {
  /** @format double */
  inletTemperature?: number;
  overTemperatureFault?: boolean;
  overTemperatureWarning?: boolean;
  /** @format double */
  pfcHeatsinkTemperature?: number;
  /** @format double */
  srHeatsinkTemperature?: number;
  /** @format int64 */
  temperaturesAgeMs?: number;
  /** @format int64 */
  thermalStateAgeMs?: number;
  thermalStateValid?: boolean;
}

export interface V2QueryMiningGroupHistoryResponse {
  buckets?: V2MiningGroupHistoryBucket[];
}

export interface V2QueryProductionHistoryResponse {
  buckets?: V2ProductionHistoryBucket[];
  identifier?: string;
  /** HistorySource identifies the retained resolution used for a response bucket. */
  source?: V2HistorySource;
}

export interface V2QueryReadOnlySQLRequest {
  /** @format int32 */
  maxRows?: number;
  sql?: string;
}

export interface V2QueryReadOnlySQLResponse {
  columns?: V2ReadOnlySQLResultColumn[];
  /** @format int64 */
  elapsedMilliseconds?: string;
  /** @format int32 */
  rowCount?: number;
  rows?: V2ReadOnlySQLRow[];
  /**
   * served_from is the lowercase database role that served the query:
   * "primary" or "standby". Older servers leave it empty; use target.role.
   */
  servedFrom?: string;
  target?: V2DiagnosticsDatabaseTarget;
  truncated?: boolean;
}

export interface V2QueryShareQualityHistoryResponse {
  buckets?: V2ShareQualityHistoryBucket[];
  grouping?: V2ShareQualityHistoryGrouping;
  /** HistorySource identifies the retained resolution used for a response bucket. */
  source?: V2HistorySource;
}

export interface V2QueryTankProductionHistoryResponse {
  buckets?: V2TankProductionBucket[];
}

export interface V2QueryTankSensorHistoryResponse {
  points?: V2TankSensorHistoryPoint[];
}

export interface V2QueryTimeSeriesResponse {
  buckets?: V2TimeSeriesBucket[];
  /**
   * TimeSeriesMetric identifies the metric type for time series queries.
   *
   *  - TIME_SERIES_METRIC_PLANT_INLET_TEMPERATURE: Plant metrics are container-scoped and ignore the identifier field.
   *  - TIME_SERIES_METRIC_PLANT_PHASE_POWER: Component metrics require identifier to select a phase, distribution
   * unit, fan, or power feed.
   *  - TIME_SERIES_METRIC_TEMPERATURE_SENSOR: Sensor-specific board or PSU temperature history. Requests must include a
   * TemperatureSeriesSelector; the legacy TEMPERATURE metric remains the
   * aggregate across every board sensor.
   */
  metric?: V2TimeSeriesMetric;
  unit?: string;
}

/** ReadOnlyDatabaseColumn describes one column in an allowlisted relation. */
export interface V2ReadOnlyDatabaseColumn {
  name?: string;
  nullable?: boolean;
  postgresType?: string;
}

/** ReadOnlyDatabaseTable describes one allowlisted relation. */
export interface V2ReadOnlyDatabaseTable {
  columns?: V2ReadOnlyDatabaseColumn[];
  name?: string;
  /**
   * DiagnosticsDatabaseRouting identifies whether a relation can be read from a
   * physical standby without losing non-WAL-replicated data.
   */
  routing?: V2DiagnosticsDatabaseRouting;
  schema?: string;
}

/**
 * ReadOnlySQLCell preserves PostgreSQL text encoding without losing numeric
 * precision and represents SQL NULL explicitly.
 */
export interface V2ReadOnlySQLCell {
  isNull?: boolean;
  text?: string;
}

/** ReadOnlySQLResultColumn identifies one result column. */
export interface V2ReadOnlySQLResultColumn {
  name?: string;
  postgresType?: string;
}

export interface V2ReadOnlySQLRow {
  cells?: V2ReadOnlySQLCell[];
}

export interface V2RebootResponse {
  message?: string;
}

/**
 * RecordedHashboardInventory contains the device's latest recorded static
 * metadata for one hashboard. It is display-only evidence: it does not prove
 * that the hashboard is currently present in the recorded slot and must never
 * authorize a board-scoped read or mutation. Current serial identity is
 * deliberately absent.
 */
export interface V2RecordedHashboardInventory {
  apiVersion?: string;
  asicModel?: string;
  chipId?: string;
  /** @format int64 */
  expectedAsicCount?: number;
  /**
   * firmware_build_id is the recorded immutable firmware build identity when
   * the device reports one independently of the display version.
   */
  firmwareBuildId?: string;
  firmwareVersion?: string;
  hardwareRevision?: string;
  serialNumber: string;
  /**
   * slot is the one-based physical position associated with this record.
   * @format int64
   */
  slot: number;
}

/** RecoveryEvidenceSummary describes one evidence snapshot without its payload. */
export interface V2RecoveryEvidenceSummary {
  /** @format date-time */
  capturedAt?: string;
  /** captured_by is the HA server ID of the node that took the snapshot. */
  capturedBy?: string;
  /** causes are the active causes that made the board eligible for recovery. */
  causes?: V2HashboardErrorCause[];
  /** episode_id links the snapshot to the recovery episode or power cycle. */
  episodeId?: string;
  /** evidence_id is the stable snapshot identifier. */
  evidenceId?: string;
  macAddress?: string;
  /**
   * payload_bytes is the stored JSON payload size.
   * @format int64
   */
  payloadBytes?: number;
  /** trigger is power_recovery, power_recovery_capture_only, or ready_recovery. */
  trigger?: string;
  /** truncated reports that the payload dropped data to stay within its cap. */
  truncated?: boolean;
}

export interface V2RefreshTokenRequest {
  /** token is the refresh token being exchanged. */
  token?: string;
}

export interface V2RefreshTokenResponse {
  /**
   * expires_at is the access-token expiry retained for protocol compatibility.
   * @format date-time
   */
  expiresAt?: string;
  /** refresh_token is populated when the server rotates the refresh token. */
  refreshToken?: string;
  /** @format date-time */
  refreshTokenExpiresAt?: string;
  /** token is the new access token retained for protocol compatibility. */
  token?: string;
}

export interface V2ReplaceMiningGroupPoolsResponse {
  group?: V2MiningGroupRecord;
  pools?: V2MiningGroupPoolRecord[];
}

export interface V2RequestMiningGroupTransferRequest {
  /** @format int64 */
  destinationExpectedRevision?: string;
  destinationGroupId?: string;
  idempotencyKey?: string;
  macAddresses?: string[];
  /** @format int64 */
  sourceExpectedRevision?: string;
  /** Empty for explicit adoption of verified unassigned boards. */
  sourceGroupId?: string;
}

export interface V2RequestMiningGroupTransferResponse {
  operation?: V2MiningGroupOperationRecord;
}

export interface V2RequestWiringVerificationRequest {
  macAddresses?: string[];
}

export interface V2RequestWiringVerificationResponse {
  queued?: boolean;
}

export type V2ResetContainerAlarmRequest = object;

export type V2ResetContainerAlarmResponse = object;

export interface V2RetryFirmwarePolicyBoardsRequest {
  /**
   * BoardScope selects hashboards. identifiers are MAC addresses or serials; modules
   * selects whole container modules by tank/module. The selections union (OR); at
   * least one is required.
   */
  scope?: V2BoardScope;
}

export interface V2RetryFirmwarePolicyBoardsResponse {
  boards?: V2FirmwarePolicyBoard[];
  policy?: V2FirmwarePolicy;
}

export interface V2RetryPoolConnectionSharingProfileRequest {
  expectedQuarantineFingerprint?: string;
  /** @format int64 */
  expectedRevision?: string;
  idempotencyKey?: string;
  profileKey?: string;
  requalificationEvidenceRef?: string;
}

export interface V2RetryPoolConnectionSharingProfileResponse {
  state?: V2PoolConnectionSharingState;
}

export interface V2RunDryCoolerFanReverseRequest {
  /** @format int64 */
  durationMinutes?: number;
}

export interface V2RunDryCoolerFanReverseResponse {
  status?: V2DryCoolerFanReverseStatus;
}

/**
 * RunInArtifactDetail controls the telemetry detail persisted in a result artifact.
 * @default "RUN_IN_ARTIFACT_DETAIL_UNSPECIFIED"
 */
export enum V2RunInArtifactDetail {
  RUN_IN_ARTIFACT_DETAIL_UNSPECIFIED = "RUN_IN_ARTIFACT_DETAIL_UNSPECIFIED",
  RUN_IN_ARTIFACT_DETAIL_FULL = "RUN_IN_ARTIFACT_DETAIL_FULL",
  RUN_IN_ARTIFACT_DETAIL_SUMMARY = "RUN_IN_ARTIFACT_DETAIL_SUMMARY",
}

/**
 * RunInBoardStatus is a module member's independent run-in state.
 * @default "RUN_IN_BOARD_STATUS_UNSPECIFIED"
 */
export enum V2RunInBoardStatus {
  RUN_IN_BOARD_STATUS_UNSPECIFIED = "RUN_IN_BOARD_STATUS_UNSPECIFIED",
  RUN_IN_BOARD_STATUS_QUEUED = "RUN_IN_BOARD_STATUS_QUEUED",
  RUN_IN_BOARD_STATUS_STARTING = "RUN_IN_BOARD_STATUS_STARTING",
  RUN_IN_BOARD_STATUS_RUNNING = "RUN_IN_BOARD_STATUS_RUNNING",
  RUN_IN_BOARD_STATUS_RECOVERING = "RUN_IN_BOARD_STATUS_RECOVERING",
  RUN_IN_BOARD_STATUS_COMPLETED = "RUN_IN_BOARD_STATUS_COMPLETED",
  RUN_IN_BOARD_STATUS_COMPLETED_WITH_WARNINGS = "RUN_IN_BOARD_STATUS_COMPLETED_WITH_WARNINGS",
  RUN_IN_BOARD_STATUS_FAILED = "RUN_IN_BOARD_STATUS_FAILED",
  RUN_IN_BOARD_STATUS_CANCELED = "RUN_IN_BOARD_STATUS_CANCELED",
}

/** RunInSourceCoverage reports one HA owner's read capability and reachability. */
export interface V2RunInSourceCoverage {
  error?: string;
  reachable?: boolean;
  serverId?: string;
  supported?: boolean;
}

/** RunInTaskBoard is one board's participation state in a module run-in. */
export interface V2RunInTaskBoard {
  macAddress?: string;
  /** @format int64 */
  pointsMissed?: number;
  /** @format int64 */
  pointsRun?: number;
  reason?: string;
  /** @format int64 */
  recoveries?: number;
  serial?: string;
  /** RunInBoardStatus is a module member's independent run-in state. */
  status?: V2RunInBoardStatus;
}

/** SSHConfig contains SSH daemon configuration. */
export interface V2SSHConfig {
  authorizedKeys?: string[];
  enabled?: boolean;
  /** @format int32 */
  port?: number;
}

/**
 * SetContainerControlResponse preserves the command echo and adds independent
 * controller-register confirmation. Confirmation is not physical feedback.
 */
export interface V2SetContainerControlResponse {
  /**
   * ContainerControlConfirmation records one fresh register read after a write.
   * Cached or echoed desired values never establish confirmation.
   */
  confirmation?: V2ContainerControlConfirmation;
  /** ContainerControl is one boolean tank/container plant control. */
  control?: V2ContainerControl;
}

export interface V2SetContainerMiningSuspendedRequest {
  /** @format int64 */
  expectedStateRevision?: string;
  idempotencyKey?: string;
  suspended?: boolean;
}

export interface V2SetContainerMiningSuspendedResponse {
  capability?: V2MiningGroupCapability;
}

/**
 * SetContainerSetpointResponse preserves the command echo and adds independent
 * controller-register confirmation. Confirmation is not physical feedback.
 */
export interface V2SetContainerSetpointResponse {
  /**
   * ContainerSetpointConfirmation compares readback with the float32 wire value,
   * retaining the original request separately so rounding is explicit.
   */
  confirmation?: V2ContainerSetpointConfirmation;
  /** ContainerSetpoint is one writable analog tank/container setting. */
  setpoint?: V2ContainerSetpoint;
}

export interface V2SetCoolingConfigRequest {
  /** CoolingConfig contains cooling system configuration. */
  config?: V2CoolingConfig;
}

export interface V2SetCoolingConfigResponse {
  /** CoolingConfig contains cooling system configuration. */
  config?: V2CoolingConfig;
}

export interface V2SetFirmwarePolicyRequest {
  /** FirmwareArtifactRef identifies one deployable firmware artifact. */
  artifact?: V2FirmwareArtifactRef;
  build?: string;
  enabled?: boolean;
  /** @format uint64 */
  expectedGeneration?: string;
  failureMode?: V2FirmwarePolicyFailureMode;
  /** @format int64 */
  maxParallel?: number;
}

export interface V2SetFirmwarePolicyResponse {
  policy?: V2FirmwarePolicy;
}

export interface V2SetFixedVoltageRequest {
  /**
   * millivolts must be between 26,000 and 30,000 inclusive.
   * @format int64
   */
  millivolts?: number;
}

export interface V2SetFixedVoltageResponse {
  /** @format int32 */
  boardsUpdated?: number;
  /** @format int64 */
  millivolts?: number;
}

export interface V2SetIdentifyLEDRequest {
  /** HashboardIdentifyLEDMode is the operator-requested identification LED mode. */
  mode?: V2HashboardIdentifyLEDMode;
  /**
   * BoardScope selects hashboards. identifiers are MAC addresses or serials; modules
   * selects whole container modules by tank/module. The selections union (OR); at
   * least one is required.
   */
  scope?: V2BoardScope;
}

export interface V2SetIdentifyLEDResponse {
  /** @format int32 */
  boardsUpdated?: number;
  /**
   * expires_at is set for BLINK requests and omitted for OFF requests.
   * @format date-time
   */
  expiresAt?: string;
}

export interface V2SetIsolationRequest {
  /** true = isolate, false = clear. */
  isolated?: boolean;
  /**
   * reason is an operator note (e.g. who has claimed the unit and why). It is
   * stored in the shared isolation ledger and returned by ListIsolation.
   */
  reason?: string;
  /**
   * BoardScope selects hashboards. identifiers are MAC addresses or serials; modules
   * selects whole container modules by tank/module. The selections union (OR); at
   * least one is required.
   */
  scope?: V2BoardScope;
}

export interface V2SetIsolationResponse {
  /** @format int32 */
  boardsChanged?: number;
  results?: V2BoardResult[];
}

export interface V2SetMainCircuitBreakerStateRequest {
  /** closed=true requests CLOSE; false requests OPEN. */
  closed?: boolean;
}

export interface V2SetMainCircuitBreakerStateResponse {
  /** changed is false when the controller was already at the target state. */
  changed?: boolean;
  /** closed is the authoritative controller readback after the request. */
  closed?: boolean;
}

export interface V2SetModuleFixedVoltageResponse {
  /** @format int32 */
  boardsUpdated?: number;
  results?: V2BoardResult[];
  /**
   * ModuleVoltageSetting is one durable module voltage setting. An absent
   * fixed_millivolts field means firmware automatic voltage tuning.
   */
  setting?: V2ModuleVoltageSetting;
}

export interface V2SetNetworkConfigRequest {
  /** NetworkConfig contains network interface configuration. */
  config?: V2NetworkConfig;
}

export interface V2SetNetworkConfigResponse {
  /** NetworkConfig contains network interface configuration. */
  config?: V2NetworkConfig;
}

export interface V2SetPasswordRequest {
  password?: string;
}

export type V2SetPasswordResponse = object;

export interface V2SetPoolAllocationModeRequest {
  /**
   * PoolAllocationMode is container scoped. A future board assignment strategy
   * can extend this enum without changing the existing pool credential schema.
   */
  mode?: V2PoolAllocationMode;
}

export interface V2SetPoolAllocationModeResponse {
  /**
   * PoolAllocationMode is container scoped. A future board assignment strategy
   * can extend this enum without changing the existing pool credential schema.
   */
  mode?: V2PoolAllocationMode;
}

export interface V2SetPoolConnectionSharingEnabledRequest {
  enabled?: boolean;
  /** @format int64 */
  expectedRevision?: string;
  idempotencyKey?: string;
}

export interface V2SetPoolConnectionSharingEnabledResponse {
  state?: V2PoolConnectionSharingState;
}

export interface V2SetPowerFeedStateResponse {
  /** PowerFeed contains one container power feed and optional telemetry. */
  powerFeed?: V2PowerFeed;
}

export interface V2SetPowerTargetScopedRequest {
  /**
   * pll_algorithm selects the firmware PLL tuning algorithm used with watts.
   * Unspecified preserves the historical behavior and selects NONE.
   */
  pllAlgorithm?: V2PLLAlgorithm;
  /**
   * BoardScope selects hashboards. identifiers are MAC addresses or serials; modules
   * selects whole container modules by tank/module. The selections union (OR); at
   * least one is required.
   */
  scope?: V2BoardScope;
  /**
   * watts is the manual per-board power target.
   * @format double
   */
  watts?: number;
}

export interface V2SetPowerTargetScopedResponse {
  /** @format int32 */
  boardsUpdated?: number;
  results?: V2BoardResult[];
}

export interface V2SetSSHConfigRequest {
  /** SSHConfig contains SSH daemon configuration. */
  config?: V2SSHConfig;
}

export interface V2SetSSHConfigResponse {
  /** SSHConfig contains SSH daemon configuration. */
  config?: V2SSHConfig;
}

export interface V2SetTagsRequest {
  tags?: MdkV2Tag[];
}

export interface V2SetTagsResponse {
  tags?: MdkV2Tag[];
}

export interface V2SetTargetPowerRequest {
  balanceBays?: boolean;
  hashOnDisconnect?: boolean;
  /**
   * PerformanceMode selects whether the miner prioritizes maximum hashrate or
   * efficiency within its power target. It is distinct from TuningAlgorithm.
   */
  performanceMode?: V2PerformanceMode;
  /** @format double */
  watts?: number;
}

export interface V2SetTargetPowerResponse {
  balanceBays?: boolean;
  hashOnDisconnect?: boolean;
  /**
   * PerformanceMode selects whether the miner prioritizes maximum hashrate or
   * efficiency within its power target. It is distinct from TuningAlgorithm.
   */
  performanceMode?: V2PerformanceMode;
  /**
   * watts is the applied target after validation and clamping.
   * @format double
   */
  watts?: number;
}

export interface V2SetTuningRequest {
  /** mode selects one explicit canonical algorithm. Unspecified is invalid. */
  mode?: V2TuningAlgorithm;
}

export interface V2SetTuningResponse {
  /** mode is the authoritative canonical algorithm after application. */
  mode?: V2TuningAlgorithm;
}

export interface V2SetUnitTagRequest {
  /**
   * UnitTag is an operator note for a tagged unit. kind is "module" (key = module
   * ID), "board" (key = MAC address), or "power_feed" (key = container power-feed
   * ID, e.g. pdu-1-outlet-03).
   */
  tag?: V2UnitTag;
}

export interface V2SetUnitTagResponse {
  /**
   * UnitTag is an operator note for a tagged unit. kind is "module" (key = module
   * ID), "board" (key = MAC address), or "power_feed" (key = container power-feed
   * ID, e.g. pdu-1-outlet-03).
   */
  tag?: V2UnitTag;
}

/**
 * Severity represents the severity level of an alert.
 * @default "SEVERITY_UNSPECIFIED"
 */
export enum V2Severity {
  SEVERITY_UNSPECIFIED = "SEVERITY_UNSPECIFIED",
  SEVERITY_INFO = "SEVERITY_INFO",
  SEVERITY_WARNING = "SEVERITY_WARNING",
  SEVERITY_ERROR = "SEVERITY_ERROR",
  SEVERITY_CRITICAL = "SEVERITY_CRITICAL",
}

export interface V2ShareQualityHistoryBucket {
  /** @format int64 */
  accepted?: string;
  /** @format double */
  acceptedDifficulty?: number;
  /** @format int64 */
  expectedBoards?: number;
  key?: string;
  /**
   * Present only for pool grouping. Zero is the legacy unattributed pool ID.
   * @format int64
   */
  poolId?: string;
  /** The stable categories of recorded rejected share results in this bucket. */
  rejectCategories?: V2ShareRejectCategoryCounts;
  /** @format double */
  rejectRate?: number;
  rejectReasons?: V2ShareRejectReason[];
  /** @format int64 */
  rejected?: string;
  /** @format int64 */
  reportingBoards?: number;
  /** @format double */
  reportingCoverage?: number;
  /** @format uint64 */
  sampleCount?: string;
  /** @format date-time */
  time?: string;
}

/** @default "SHARE_QUALITY_HISTORY_GROUPING_UNSPECIFIED" */
export enum V2ShareQualityHistoryGrouping {
  SHARE_QUALITY_HISTORY_GROUPING_UNSPECIFIED = "SHARE_QUALITY_HISTORY_GROUPING_UNSPECIFIED",
  SHARE_QUALITY_HISTORY_GROUPING_TOTAL = "SHARE_QUALITY_HISTORY_GROUPING_TOTAL",
  SHARE_QUALITY_HISTORY_GROUPING_BOARD = "SHARE_QUALITY_HISTORY_GROUPING_BOARD",
  SHARE_QUALITY_HISTORY_GROUPING_POOL = "SHARE_QUALITY_HISTORY_GROUPING_POOL",
}

/**
 * ShareQualityStats contains share acceptance metrics for a single grouping.
 * key is the CT1 MAC address for by_board entries, the pool name for by_pool
 * entries, and empty for the total aggregate. R2 by_board entries use serial.
 */
export interface V2ShareQualityStats {
  /** @format double */
  acceptanceRate?: number;
  /** @format int64 */
  accepted?: string;
  /**
   * accepted_hashrate_ths is the hashrate derived from accepted
   * share difficulty over the response window, in terahashes per second.
   * @format double
   */
  acceptedHashrateThs?: number;
  key?: string;
  /**
   * Present only for by_pool entries. Zero is the legacy unattributed pool ID.
   * @format int64
   */
  poolId?: string;
  /** Present for CT1 share quality; absent when categories are unsupported. */
  rejectCategories?: V2ShareRejectCategoryCounts;
  rejectReasons?: V2ShareRejectReason[];
  /** @format int64 */
  rejected?: string;
  /**
   * serial is the stable R2 board identity for by_board entries. It is empty
   * for CT1, pool, and total groupings.
   */
  serial?: string;
  /**
   * slot is present only when all samples belong to one known R2 slot.
   * @format int64
   */
  slot?: number;
}

/**
 * ShareQualityWindow selects the rolling window for share quality statistics.
 * @default "SHARE_QUALITY_WINDOW_UNSPECIFIED"
 */
export enum V2ShareQualityWindow {
  SHARE_QUALITY_WINDOW_UNSPECIFIED = "SHARE_QUALITY_WINDOW_UNSPECIFIED",
  SHARE_QUALITY_WINDOW_15M = "SHARE_QUALITY_WINDOW_15M",
  SHARE_QUALITY_WINDOW_1H = "SHARE_QUALITY_WINDOW_1H",
  SHARE_QUALITY_WINDOW_24H = "SHARE_QUALITY_WINDOW_24H",
}

/**
 * ShareRejectCategoryCounts groups recorded rejected share results without
 * requiring clients to interpret pool-specific or legacy reason strings.
 * Invalid includes invalid time, low difficulty, and invalid user. Unknown
 * includes unclassified, unfamiliar, or otherwise unattributed results.
 */
export interface V2ShareRejectCategoryCounts {
  /** @format int64 */
  duplicate?: string;
  /** @format int64 */
  invalid?: string;
  /** @format int64 */
  stale?: string;
  /** @format int64 */
  submitFailure?: string;
  /** @format int64 */
  unknown?: string;
}

/** ShareRejectReason counts share rejections grouped by recorded reason. */
export interface V2ShareRejectReason {
  /** @format int64 */
  count?: string;
  reason?: string;
}

/** Slot represents a hashboard slot within a module. */
export interface V2Slot {
  /** Fixed slot expectation, also present for an unoccupied slot. */
  configuredLocation?: V2HashboardLocation;
  /** Hashboard represents a mining hashboard with its current state and health. */
  hashboard?: V2Hashboard;
  occupied?: boolean;
  powerFeedId?: string;
}

/** SoftFeedMappingBoardResult contains the soft mapping result for one board. */
export interface V2SoftFeedMappingBoardResult {
  anomalies?: string[];
  /** @format double */
  confidence?: number;
  currentFeedId?: string;
  error?: string;
  /** SoftFeedMappingEvidence records the power-feed signal used for one board inference. */
  evidence?: V2SoftFeedMappingEvidence;
  firmwareVersion?: string;
  hwRevision?: string;
  inferredFeedId?: string;
  macAddress?: string;
  serial?: string;
  /** SoftFeedMappingBoardStatus is the per-board result of a soft mapping run. */
  status?: V2SoftFeedMappingBoardStatus;
}

/**
 * SoftFeedMappingBoardStatus is the per-board result of a soft mapping run.
 * @default "SOFT_FEED_MAPPING_BOARD_STATUS_UNSPECIFIED"
 */
export enum V2SoftFeedMappingBoardStatus {
  SOFT_FEED_MAPPING_BOARD_STATUS_UNSPECIFIED = "SOFT_FEED_MAPPING_BOARD_STATUS_UNSPECIFIED",
  SOFT_FEED_MAPPING_BOARD_STATUS_PENDING = "SOFT_FEED_MAPPING_BOARD_STATUS_PENDING",
  SOFT_FEED_MAPPING_BOARD_STATUS_RUNNING = "SOFT_FEED_MAPPING_BOARD_STATUS_RUNNING",
  SOFT_FEED_MAPPING_BOARD_STATUS_CONFIRMED = "SOFT_FEED_MAPPING_BOARD_STATUS_CONFIRMED",
  SOFT_FEED_MAPPING_BOARD_STATUS_MISMATCH = "SOFT_FEED_MAPPING_BOARD_STATUS_MISMATCH",
  SOFT_FEED_MAPPING_BOARD_STATUS_ADDED = "SOFT_FEED_MAPPING_BOARD_STATUS_ADDED",
  SOFT_FEED_MAPPING_BOARD_STATUS_REMOVED = "SOFT_FEED_MAPPING_BOARD_STATUS_REMOVED",
  SOFT_FEED_MAPPING_BOARD_STATUS_AMBIGUOUS = "SOFT_FEED_MAPPING_BOARD_STATUS_AMBIGUOUS",
  SOFT_FEED_MAPPING_BOARD_STATUS_NO_SIGNAL = "SOFT_FEED_MAPPING_BOARD_STATUS_NO_SIGNAL",
  SOFT_FEED_MAPPING_BOARD_STATUS_SKIPPED = "SOFT_FEED_MAPPING_BOARD_STATUS_SKIPPED",
  SOFT_FEED_MAPPING_BOARD_STATUS_ERROR = "SOFT_FEED_MAPPING_BOARD_STATUS_ERROR",
  SOFT_FEED_MAPPING_BOARD_STATUS_APPLIED = "SOFT_FEED_MAPPING_BOARD_STATUS_APPLIED",
}

/** SoftFeedMappingEvidence records the power-feed signal used for one board inference. */
export interface V2SoftFeedMappingEvidence {
  /** @format double */
  baselinePowerFeedWatts?: number;
  /** @format double */
  bestDropWatts?: number;
  /** @format double */
  perturbedPowerFeedWatts?: number;
  /** @format double */
  recoveryWatts?: number;
  /** @format double */
  restoredPowerFeedWatts?: number;
  /** @format double */
  secondBestDropWatts?: number;
  /** @format double */
  totalDropWatts?: number;
}

/** SoftFeedMappingRun is a snapshot of a soft feed mapping run. */
export interface V2SoftFeedMappingRun {
  /** @format int32 */
  added?: number;
  /** @format int32 */
  ambiguous?: number;
  /** @format int32 */
  applied?: number;
  applyAvailable?: boolean;
  boards?: V2SoftFeedMappingBoardResult[];
  /** @format int32 */
  changed?: number;
  /** @format date-time */
  completedAt?: string;
  /** @format int32 */
  confirmed?: number;
  currentBoardId?: string;
  error?: string;
  /** @format int32 */
  noSignal?: number;
  phase?: string;
  /** @format int32 */
  removed?: number;
  runId?: string;
  /** @format int32 */
  skipped?: number;
  /** @format date-time */
  startedAt?: string;
  /** FeedMappingRunStatus is the lifecycle state of a feed mapping run. */
  status?: V2FeedMappingRunStatus;
  /** @format date-time */
  updatedAt?: string;
  warnings?: string[];
}

/** SoftFeedMappingRunConfig configures soft mapping behavior. */
export interface V2SoftFeedMappingRunConfig {
  /**
   * If second-best drop / best drop exceeds this value, mark the result ambiguous.
   * @format double
   */
  ambiguityRatio?: number;
  /** Optional board MAC addresses or serial numbers to test. Empty means all live mining boards. */
  boardIds?: string[];
  /** Required for mutating board controls. */
  confirmBoardControl?: boolean;
  /** Optional feed IDs to consider as candidates. Empty means all configured/live feeds. */
  feedIds?: string[];
  /**
   * Restrict the run to boards controlled by this node and skip HA-peer
   * delegation. Set automatically on delegated runs from the peer's parent
   * run; the server accepts such a run on a non-primary node only when the
   * call's source address is the configured HA peer. Any other local_only
   * start (like every other start) still requires this node to be the
   * container-write primary; apply always does.
   */
  localOnly?: boolean;
  /**
   * Minimum power-feed watt drop needed to infer a mapping.
   * @format double
   */
  minDropWatts?: number;
  /** Delay after stopping the board before measuring the power drop. */
  perturbSettle?: string;
  /** Delay after restarting the board before measuring recovery. */
  restoreSettle?: string;
  /**
   * Number of power-feed samples to average for each measurement window.
   * @format int32
   */
  sampleCount?: number;
  /** Delay between power-feed samples. */
  sampleInterval?: string;
  /** Internal fenced verification context, validated against the shared lease. */
  verificationToken?: string;
  wiringGeneration?: string;
}

/** SoftwareComponentVersion identifies one product software component. */
export interface V2SoftwareComponentVersion {
  name?: string;
  version?: string;
}

/**
 * SoftwareUpdateState identifies the product software update lifecycle.
 * @default "SOFTWARE_UPDATE_STATE_UNSPECIFIED"
 */
export enum V2SoftwareUpdateState {
  SOFTWARE_UPDATE_STATE_UNSPECIFIED = "SOFTWARE_UPDATE_STATE_UNSPECIFIED",
  SOFTWARE_UPDATE_STATE_CURRENT = "SOFTWARE_UPDATE_STATE_CURRENT",
  SOFTWARE_UPDATE_STATE_AVAILABLE = "SOFTWARE_UPDATE_STATE_AVAILABLE",
  SOFTWARE_UPDATE_STATE_DOWNLOADING = "SOFTWARE_UPDATE_STATE_DOWNLOADING",
  SOFTWARE_UPDATE_STATE_DOWNLOADED = "SOFTWARE_UPDATE_STATE_DOWNLOADED",
  SOFTWARE_UPDATE_STATE_INSTALLING = "SOFTWARE_UPDATE_STATE_INSTALLING",
  SOFTWARE_UPDATE_STATE_INSTALLED = "SOFTWARE_UPDATE_STATE_INSTALLED",
  SOFTWARE_UPDATE_STATE_CONFIRMING = "SOFTWARE_UPDATE_STATE_CONFIRMING",
  SOFTWARE_UPDATE_STATE_SUCCESS = "SOFTWARE_UPDATE_STATE_SUCCESS",
  SOFTWARE_UPDATE_STATE_ERROR = "SOFTWARE_UPDATE_STATE_ERROR",
}

/** SoftwareUpdateStatus contains current and available product image status. */
export interface V2SoftwareUpdateStatus {
  availableVersion?: string;
  currentVersion?: string;
  error?: string;
  message?: string;
  /**
   * R2 update ID; GetSystemInfo exposes the active or latest retained ID.
   * Empty when unavailable.
   */
  operationId?: string;
  previousVersion?: string;
  /** @format int64 */
  progressPercent?: number;
  releaseNotes?: string;
  /** SoftwareUpdateState identifies the product software update lifecycle. */
  state?: V2SoftwareUpdateState;
}

export interface V2StartFeedMappingRunResponse {
  /** FeedMappingRun is a snapshot of a feed mapping run. */
  run?: V2FeedMappingRun;
  runId?: string;
}

export interface V2StartFirmwareUpdateRequest {
  /**
   * r2_targets selects R2 boards by stable serial number. R2 must provide at
   * least one selector and leave scope unset. Exactly one of scope or
   * r2_targets is valid. Empty, mixed, duplicate, incomplete, or stale
   * selection must fail explicitly.
   */
  r2Targets?: V2FirmwareTargetSelector[];
  /** FirmwareArtifactRef identifies one deployable firmware artifact. */
  artifact?: V2FirmwareArtifactRef;
  /**
   * FirmwareUpdateOptions tune one update job. Proto3 zero values give the safe
   * defaults: stop mining before flashing and restore it afterwards.
   */
  options?: V2FirmwareUpdateOptions;
  /**
   * scope is retained for CT1 compatibility. CT1 must provide scope and leave
   * r2_targets empty.
   */
  scope?: V2BoardScope;
  /**
   * task_id is optional and used by HA peers so sibling jobs from one cluster
   * deployment share a visible update identifier. Ordinary clients leave it empty.
   */
  taskId?: string;
}

export interface V2StartFirmwareUpdateResponse {
  /** FirmwareUpdate is one firmware update job. */
  update?: V2FirmwareUpdate;
}

export interface V2StartHashboardRunInResponse {
  macAddress?: string;
  serial?: string;
  /** HashboardRunInStatus is the lifecycle state of a run-in task. */
  status?: V2HashboardRunInStatus;
  taskId?: string;
}

export interface V2StartManufacturingSelfTestRequest {
  identifier?: string;
  /** local_only is reserved for trusted HA peer delegation. */
  localOnly?: boolean;
}

export interface V2StartManufacturingSelfTestResponse {
  test?: V2ManufacturingSelfTest;
}

export type V2StartMiningResponse = object;

export interface V2StartMiningScopedRequest {
  /**
   * BoardScope selects hashboards. identifiers are MAC addresses or serials; modules
   * selects whole container modules by tank/module. The selections union (OR); at
   * least one is required.
   */
  scope?: V2BoardScope;
}

export interface V2StartMiningScopedResponse {
  /** @format int32 */
  boardsStarted?: number;
  results?: V2BoardResult[];
}

export interface V2StartModuleRunInResponse {
  boards?: V2RunInTaskBoard[];
  moduleId?: string;
  /** HashboardRunInStatus is the lifecycle state of a run-in task. */
  status?: V2HashboardRunInStatus;
  taskId?: string;
  voltageLeaderMac?: string;
}

export interface V2StartPSUFirmwareUpdateRequest {
  capsuleId?: string;
  /** force reapplies an exact capsule that already succeeded. */
  force?: boolean;
  moduleId?: string;
  /**
   * recover restages the exact capsule and resumes the leader's recovery
   * journal instead of starting a new update.
   */
  recover?: boolean;
}

export interface V2StartPSUFirmwareUpdateResponse {
  /** PSUFirmwareUpdate is one module update job. */
  update?: V2PSUFirmwareUpdate;
}

export interface V2StartSoftFeedMappingRunResponse {
  /** SoftFeedMappingRun is a snapshot of a soft feed mapping run. */
  run?: V2SoftFeedMappingRun;
  runId?: string;
}

export type V2StopMiningResponse = object;

export interface V2StopMiningScopedRequest {
  /**
   * BoardScope selects hashboards. identifiers are MAC addresses or serials; modules
   * selects whole container modules by tank/module. The selections union (OR); at
   * least one is required.
   */
  scope?: V2BoardScope;
}

export interface V2StopMiningScopedResponse {
  /** @format int32 */
  boardsStopped?: number;
  results?: V2BoardResult[];
}

/** StreamASICHealthResponse contains per-ASIC health data for a hashboard. */
export interface V2StreamASICHealthResponse {
  asics?: V2ASIC[];
  /** layout is present on the first frame and whenever the physical topology changes. */
  layout?: V2HashboardASICLayout;
  /**
   * mac_address is the legacy CT1 board identity. It is empty for R2 and must
   * never contain a synthesized address.
   */
  macAddress?: string;
  /** serial is the stable R2 board identity. */
  serial?: string;
  /**
   * slot is the optional one-based R2 chassis slot.
   * @format int64
   */
  slot?: number;
  /** @format date-time */
  timestamp?: string;
}

export interface V2StreamAlertsResponse {
  /** Alert represents a system alert. */
  alert?: V2Alert;
}

/** StreamContainerHealthResponse contains plant and power-feed telemetry. */
export interface V2StreamContainerHealthResponse {
  /** PlantHealth contains normalized optional container-plant telemetry. */
  plant?: V2PlantHealth;
  powerFeeds?: V2PowerFeed[];
  /** @format date-time */
  timestamp?: string;
}

/** StreamDeviceMetricsResponse contains aggregate device-level metrics. */
export interface V2StreamDeviceMetricsResponse {
  /** @format int32 */
  activeBoards?: number;
  /** @format double */
  avgTemperature?: number;
  boardsByState?: Record<string, number>;
  /** @format double */
  efficiency?: number;
  /** @format double */
  hashrate?: number;
  /** @format double */
  power?: number;
  /** @format date-time */
  timestamp?: string;
}

/** StreamHashboardHealthResponse contains a health snapshot for a single hashboard. */
export interface V2StreamHashboardHealthResponse {
  /** HashboardHealth contains real-time health metrics for a hashboard. */
  health?: V2HashboardHealth;
  /**
   * mac_address is the legacy CT1 board identity. It is empty for R2 and must
   * never contain a synthesized address.
   */
  macAddress?: string;
  /** MiningState represents the operational state of a hashboard. */
  miningState?: V2MiningState;
  /** serial is the stable R2 board identity. */
  serial?: string;
  /**
   * slot is the optional one-based R2 chassis slot.
   * @format int64
   */
  slot?: number;
}

/** SystemInfo contains static system information. */
export interface V2SystemInfo {
  componentVersions?: V2SoftwareComponentVersion[];
  /** ControlBoardInfo contains stable control-board hardware identity. */
  controlBoard?: V2ControlBoardInfo;
  hostname?: string;
  id?: string;
  /** IpAddresses contains active non-loopback host addresses. */
  ipAddresses?: string[];
  manufacturer?: string;
  model?: string;
  /** OSBuildInfo contains immutable operating-system image identity. */
  osBuild?: V2OSBuildInfo;
  product?: string;
  /**
   * Resources is an optional point-in-time host and process sample. Older
   * implementations omit it while preserving the static identity fields.
   */
  resources?: V2SystemResources;
  /** SoftwareUpdateStatus contains current and available product image status. */
  softwareUpdate?: V2SoftwareUpdateStatus;
  /** @format int64 */
  uptimeSeconds?: string;
  version?: string;
}

/** SystemResources contains one point-in-time host and Frigate process sample. */
export interface V2SystemResources {
  /** @format double */
  hostCpuUsagePercent?: number;
  /** @format uint64 */
  hostDiskTotalBytes?: string;
  /**
   * Host disk values describe the root filesystem visible to Frigate.
   * @format uint64
   */
  hostDiskUsedBytes?: string;
  /** @format int64 */
  hostLogicalCpuCount?: number;
  /** @format uint64 */
  hostMemoryTotalBytes?: string;
  /** @format uint64 */
  hostMemoryUsedBytes?: string;
  /** @format date-time */
  observedAt?: string;
  /** @format double */
  processCpuUsageCores?: number;
  /** @format double */
  processCpuUsagePercent?: number;
  /** @format uint64 */
  processMemoryBytes?: string;
}

/** SystemStatus contains current system status. */
export interface V2SystemStatus {
  /** @format int32 */
  activeBoards?: number;
  activePool?: string;
  containerWritePrimary?: boolean;
  databaseConnected?: boolean;
  /**
   * DcsHealthy reports whether the latest distributed-configuration-store
   * snapshot read succeeded and validated. It is absent for standalone
   * deployments and older Frigate versions that do not publish DCS health.
   */
  dcsHealthy?: boolean;
  /**
   * FlotillaOperationalHealth is populated by Flotilla from its retained
   * Frigate status. It is independent of health, whose publication boundary
   * remains unchanged for legacy clients, and is absent on older servers.
   */
  flotillaOperationalHealth?: V2HealthStatus;
  /**
   * FlotillaTunnelConnected is populated by Flotilla when projecting a
   * container. It reports raw tunnel transport independently from the legacy
   * composite Container.connection field and is absent on older servers.
   */
  flotillaTunnelConnected?: boolean;
  /**
   * HaMembers reports this server's cached, bounded probe of the distributed
   * configuration store voters and the configured etcd witness host. Each HA
   * peer probes independently, so peers can disagree during a partition. An
   * empty list means the deployment does not publish HA member probes.
   */
  haMembers?: V2HAMember[];
  /**
   * HARecoveryStatus is bounded, machine-readable progress for one post-Patroni
   * recovery episode. It is process-local status backed by shared assignment
   * authority; it is not a durable workflow record.
   */
  haRecovery?: V2HARecoveryStatus;
  /** HARole identifies this server's role in a high-availability cluster. */
  haRole?: V2HARole;
  /** HealthStatus represents the overall health of the system. */
  health?: V2HealthStatus;
  /**
   * HostDiagnostics reports additive, locally observed host safety signals.
   * Older Frigate/Flotilla versions omit it, so consumers must render absence
   * as "not reported" rather than healthy.
   */
  hostDiagnostics?: V2HostDiagnostics;
  mining?: boolean;
  serverId?: string;
  /** @format int32 */
  totalBoards?: number;
  version?: string;
}

/**
 * SystemUpdateOptions defaults to preserving settings and manual reboot.
 * Options are process-local; a server restart drops automatic reboot/reset intent.
 */
export interface V2SystemUpdateOptions {
  /** Reboot automatically after successful installation. */
  autoReboot?: boolean;
  /**
   * Clear user settings with the platform reset reboot after successful installation.
   * Requires auto_reboot; otherwise the request returns FAILED_PRECONDITION.
   * On R2 this clears /data, including API credentials and update history.
   */
  clearUserSettings?: boolean;
}

/** Tank represents a physical tank in the container. */
export interface V2Tank {
  /**
   * display_label is the optional operator-facing tank name, for example
   * "Tank 4 (door)". Empty means clients show "Tank <display number>".
   */
  displayLabel?: string;
  /**
   * display_number is the operator-facing tank number from the optional
   * container tank_display configuration. Zero means unset, and clients then
   * display id. It is presentation only: id remains the identity used by
   * hashboard locations, power-feed IDs, PLC addressing, and stored data.
   * @format int32
   */
  displayNumber?: number;
  /** @format int32 */
  id?: number;
  /**
   * module_layout describes the physical module grid for this tank. Only
   * mapped modules appear in modules; grid gaps remain meaningful positions.
   */
  moduleLayout?: V2ModuleLayout;
  modules?: V2Module[];
}

export interface V2TankProductionBucket {
  /** @format double */
  hashrateTh?: number;
  /** @format double */
  powerW?: number;
  /** @format int64 */
  reportingBoards?: number;
  /** @format date-time */
  start?: string;
}

export interface V2TankSensorHistoryPoint {
  /** @format date-time */
  acquiredAt?: string;
  bootId?: string;
  calibrationValidated?: boolean;
  configurationGeneration?: string;
  controllerId?: string;
  /** @format date-time */
  receivedAt?: string;
  /** @format int64 */
  sampleAgeMs?: number;
  /** @format uint64 */
  sampleSeq?: string;
  sensorId?: string;
  status?: string;
  tankKey?: string;
  /** @format date-time */
  time?: string;
  unit?: string;
  valid?: boolean;
  /** @format double */
  value?: number;
}

export interface V2TestMiningGroupPoolConnectionResponse {
  error?: string;
  /** @format double */
  latencyMs?: number;
  success?: boolean;
}

export interface V2TestPoolConnectionResponse {
  error?: string;
  /** @format double */
  latencyMs?: number;
  success?: boolean;
}

/** TimeRange specifies a time window for queries. */
export interface V2TimeRange {
  /** @format date-time */
  end?: string;
  /** @format date-time */
  start?: string;
}

/** TimeSeriesBucket contains aggregated metrics for a time window. */
export interface V2TimeSeriesBucket {
  /** @format double */
  avg?: number;
  /** @format int64 */
  count?: string;
  coverageComplete?: boolean;
  /** @format int64 */
  expectedContributors?: number;
  /** @format double */
  max?: number;
  /** @format double */
  min?: number;
  /** @format int64 */
  reportingContributors?: number;
  /** HistorySource identifies the retained resolution used for a response bucket. */
  source?: V2HistorySource;
  /** @format date-time */
  time?: string;
}

/**
 * TimeSeriesMetric identifies the metric type for time series queries.
 *
 *  - TIME_SERIES_METRIC_PLANT_INLET_TEMPERATURE: Plant metrics are container-scoped and ignore the identifier field.
 *  - TIME_SERIES_METRIC_PLANT_PHASE_POWER: Component metrics require identifier to select a phase, distribution
 * unit, fan, or power feed.
 *  - TIME_SERIES_METRIC_TEMPERATURE_SENSOR: Sensor-specific board or PSU temperature history. Requests must include a
 * TemperatureSeriesSelector; the legacy TEMPERATURE metric remains the
 * aggregate across every board sensor.
 * @default "TIME_SERIES_METRIC_UNSPECIFIED"
 */
export enum V2TimeSeriesMetric {
  TIME_SERIES_METRIC_UNSPECIFIED = "TIME_SERIES_METRIC_UNSPECIFIED",
  TIME_SERIES_METRIC_HASHRATE = "TIME_SERIES_METRIC_HASHRATE",
  TIME_SERIES_METRIC_POWER = "TIME_SERIES_METRIC_POWER",
  TIME_SERIES_METRIC_TEMPERATURE = "TIME_SERIES_METRIC_TEMPERATURE",
  TIME_SERIES_METRIC_VOLTAGE = "TIME_SERIES_METRIC_VOLTAGE",
  TIME_SERIES_METRIC_PLANT_INLET_TEMPERATURE = "TIME_SERIES_METRIC_PLANT_INLET_TEMPERATURE",
  TIME_SERIES_METRIC_PLANT_OUTLET_TEMPERATURE = "TIME_SERIES_METRIC_PLANT_OUTLET_TEMPERATURE",
  TIME_SERIES_METRIC_PLANT_POWER = "TIME_SERIES_METRIC_PLANT_POWER",
  TIME_SERIES_METRIC_PLANT_AMBIENT_TEMPERATURE = "TIME_SERIES_METRIC_PLANT_AMBIENT_TEMPERATURE",
  TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_POWER = "TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_POWER",
  TIME_SERIES_METRIC_PLANT_FACILITY_POWER = "TIME_SERIES_METRIC_PLANT_FACILITY_POWER",
  TIME_SERIES_METRIC_PLANT_REACTIVE_POWER = "TIME_SERIES_METRIC_PLANT_REACTIVE_POWER",
  TIME_SERIES_METRIC_PLANT_APPARENT_POWER = "TIME_SERIES_METRIC_PLANT_APPARENT_POWER",
  TIME_SERIES_METRIC_PLANT_ACTIVE_ENERGY = "TIME_SERIES_METRIC_PLANT_ACTIVE_ENERGY",
  TIME_SERIES_METRIC_PLANT_REACTIVE_ENERGY = "TIME_SERIES_METRIC_PLANT_REACTIVE_ENERGY",
  TIME_SERIES_METRIC_PLANT_APPARENT_ENERGY = "TIME_SERIES_METRIC_PLANT_APPARENT_ENERGY",
  TIME_SERIES_METRIC_PLANT_POWER_FACTOR = "TIME_SERIES_METRIC_PLANT_POWER_FACTOR",
  TIME_SERIES_METRIC_PLANT_PUE = "TIME_SERIES_METRIC_PLANT_PUE",
  TIME_SERIES_METRIC_PLANT_DRY_COOLER_INLET_TEMPERATURE = "TIME_SERIES_METRIC_PLANT_DRY_COOLER_INLET_TEMPERATURE",
  TIME_SERIES_METRIC_PLANT_OUTLET_A_TEMPERATURE = "TIME_SERIES_METRIC_PLANT_OUTLET_A_TEMPERATURE",
  TIME_SERIES_METRIC_PLANT_OUTLET_B_TEMPERATURE = "TIME_SERIES_METRIC_PLANT_OUTLET_B_TEMPERATURE",
  TIME_SERIES_METRIC_PLANT_PUMP_OUTLET_PRESSURE = "TIME_SERIES_METRIC_PLANT_PUMP_OUTLET_PRESSURE",
  TIME_SERIES_METRIC_PLANT_PUMP_INLET_PRESSURE = "TIME_SERIES_METRIC_PLANT_PUMP_INLET_PRESSURE",
  TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_INLET_PRESSURE = "TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_INLET_PRESSURE",
  TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_OUTLET_PRESSURE = "TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_OUTLET_PRESSURE",
  TIME_SERIES_METRIC_PLANT_TANK_A_LIQUID_LEVEL = "TIME_SERIES_METRIC_PLANT_TANK_A_LIQUID_LEVEL",
  TIME_SERIES_METRIC_PLANT_TANK_B_LIQUID_LEVEL = "TIME_SERIES_METRIC_PLANT_TANK_B_LIQUID_LEVEL",
  TIME_SERIES_METRIC_PLANT_FLOW_RATE = "TIME_SERIES_METRIC_PLANT_FLOW_RATE",
  TIME_SERIES_METRIC_PLANT_AMBIENT_HUMIDITY = "TIME_SERIES_METRIC_PLANT_AMBIENT_HUMIDITY",
  TIME_SERIES_METRIC_PLANT_FILTER_PRESSURE_DIFFERENTIAL = "TIME_SERIES_METRIC_PLANT_FILTER_PRESSURE_DIFFERENTIAL",
  TIME_SERIES_METRIC_PLANT_PUMP_PRESSURE_DIFFERENTIAL = "TIME_SERIES_METRIC_PLANT_PUMP_PRESSURE_DIFFERENTIAL",
  TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_PRESSURE_DIFFERENTIAL = "TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_PRESSURE_DIFFERENTIAL",
  TIME_SERIES_METRIC_PLANT_COOLANT_PUMP_SPEED = "TIME_SERIES_METRIC_PLANT_COOLANT_PUMP_SPEED",
  TIME_SERIES_METRIC_PLANT_PHASE_POWER = "TIME_SERIES_METRIC_PLANT_PHASE_POWER",
  TIME_SERIES_METRIC_PLANT_PHASE_CURRENT = "TIME_SERIES_METRIC_PLANT_PHASE_CURRENT",
  TIME_SERIES_METRIC_PLANT_PHASE_VOLTAGE = "TIME_SERIES_METRIC_PLANT_PHASE_VOLTAGE",
  TIME_SERIES_METRIC_PLANT_DISTRIBUTION_ACTIVE_FEEDS = "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_ACTIVE_FEEDS",
  TIME_SERIES_METRIC_PLANT_DISTRIBUTION_VOLTAGE_AB = "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_VOLTAGE_AB",
  TIME_SERIES_METRIC_PLANT_DISTRIBUTION_VOLTAGE_BC = "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_VOLTAGE_BC",
  TIME_SERIES_METRIC_PLANT_DISTRIBUTION_VOLTAGE_CA = "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_VOLTAGE_CA",
  TIME_SERIES_METRIC_PLANT_DISTRIBUTION_CURRENT_A = "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_CURRENT_A",
  TIME_SERIES_METRIC_PLANT_DISTRIBUTION_CURRENT_B = "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_CURRENT_B",
  TIME_SERIES_METRIC_PLANT_DISTRIBUTION_CURRENT_C = "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_CURRENT_C",
  TIME_SERIES_METRIC_PLANT_DISTRIBUTION_POWER = "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_POWER",
  TIME_SERIES_METRIC_PLANT_DISTRIBUTION_ACTIVE_ENERGY = "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_ACTIVE_ENERGY",
  TIME_SERIES_METRIC_PLANT_FAN_POWER = "TIME_SERIES_METRIC_PLANT_FAN_POWER",
  TIME_SERIES_METRIC_PLANT_FAN_SPEED = "TIME_SERIES_METRIC_PLANT_FAN_SPEED",
  TIME_SERIES_METRIC_POWER_FEED_POWER = "TIME_SERIES_METRIC_POWER_FEED_POWER",
  TIME_SERIES_METRIC_POWER_FEED_CURRENT = "TIME_SERIES_METRIC_POWER_FEED_CURRENT",
  TIME_SERIES_METRIC_POWER_FEED_VOLTAGE = "TIME_SERIES_METRIC_POWER_FEED_VOLTAGE",
  TIME_SERIES_METRIC_POWER_FEED_VOLTAGE_AB = "TIME_SERIES_METRIC_POWER_FEED_VOLTAGE_AB",
  TIME_SERIES_METRIC_POWER_FEED_VOLTAGE_BC = "TIME_SERIES_METRIC_POWER_FEED_VOLTAGE_BC",
  TIME_SERIES_METRIC_POWER_FEED_VOLTAGE_CA = "TIME_SERIES_METRIC_POWER_FEED_VOLTAGE_CA",
  TIME_SERIES_METRIC_POWER_FEED_CURRENT_A = "TIME_SERIES_METRIC_POWER_FEED_CURRENT_A",
  TIME_SERIES_METRIC_POWER_FEED_CURRENT_B = "TIME_SERIES_METRIC_POWER_FEED_CURRENT_B",
  TIME_SERIES_METRIC_POWER_FEED_CURRENT_C = "TIME_SERIES_METRIC_POWER_FEED_CURRENT_C",
  TIME_SERIES_METRIC_TEMPERATURE_SENSOR = "TIME_SERIES_METRIC_TEMPERATURE_SENSOR",
  TIME_SERIES_METRIC_PLANT_INSIDE_AMBIENT_TEMPERATURE = "TIME_SERIES_METRIC_PLANT_INSIDE_AMBIENT_TEMPERATURE",
  TIME_SERIES_METRIC_PLANT_INSIDE_AMBIENT_HUMIDITY = "TIME_SERIES_METRIC_PLANT_INSIDE_AMBIENT_HUMIDITY",
  TIME_SERIES_METRIC_PLANT_OUTSIDE_AMBIENT_TEMPERATURE = "TIME_SERIES_METRIC_PLANT_OUTSIDE_AMBIENT_TEMPERATURE",
  TIME_SERIES_METRIC_PLANT_OUTSIDE_AMBIENT_HUMIDITY = "TIME_SERIES_METRIC_PLANT_OUTSIDE_AMBIENT_HUMIDITY",
}

/**
 * TuningAlgorithm names the canonical mining tuning algorithm shared by all
 * products. Implementations map these semantic values to their native tuning
 * engine without exposing product-specific enum fields on the MDK API.
 * @default "TUNING_ALGORITHM_UNSPECIFIED"
 */
export enum V2TuningAlgorithm {
  TUNING_ALGORITHM_UNSPECIFIED = "TUNING_ALGORITHM_UNSPECIFIED",
  TUNING_ALGORITHM_NONE = "TUNING_ALGORITHM_NONE",
  TUNING_ALGORITHM_VOLTAGE_BALANCE = "TUNING_ALGORITHM_VOLTAGE_BALANCE",
  TUNING_ALGORITHM_TEMPERATURE_GRADIENT_WITH_VOLTAGE_BALANCE = "TUNING_ALGORITHM_TEMPERATURE_GRADIENT_WITH_VOLTAGE_BALANCE",
  TUNING_ALGORITHM_FMARGIN = "TUNING_ALGORITHM_FMARGIN",
  TUNING_ALGORITHM_DEBUG = "TUNING_ALGORITHM_DEBUG",
  TUNING_ALGORITHM_FMARGIN_THERMAL_THROTTLE = "TUNING_ALGORITHM_FMARGIN_THERMAL_THROTTLE",
  TUNING_ALGORITHM_FMARGIN_DELTA_THERMAL_THROTTLE = "TUNING_ALGORITHM_FMARGIN_DELTA_THERMAL_THROTTLE",
}

/** TuningSweep configures fixed-voltage points crossed with either fixed-frequency or power-target/PLL-algorithm points. */
export interface V2TuningSweep {
  dwellDuration?: string;
  /** TuningSweepFailurePolicy controls sweep behavior when applying a point fails. */
  failurePolicy?: V2TuningSweepFailurePolicy;
  /** FrequencyRange defines requested ASIC PLL frequency sweep points in MHz. */
  frequency?: V2FrequencyRange;
  pllAlgorithms?: V2PLLAlgorithm[];
  /** PowerTargetRange defines requested board power-target sweep points in watts. */
  powerTarget?: V2PowerTargetRange;
  /**
   * Restart AUTO voltage tuning after applying each point's power/PLL settings.
   * Defaults to false; ignored when a fixed voltage range is supplied.
   */
  restartVoltageTuningEachPoint?: boolean;
  /** VoltageRange defines requested PSU voltage sweep points in volts. */
  voltage?: V2VoltageRange;
}

/**
 * TuningSweepFailurePolicy controls sweep behavior when applying a point fails.
 * @default "TUNING_SWEEP_FAILURE_POLICY_UNSPECIFIED"
 */
export enum V2TuningSweepFailurePolicy {
  TUNING_SWEEP_FAILURE_POLICY_UNSPECIFIED = "TUNING_SWEEP_FAILURE_POLICY_UNSPECIFIED",
  TUNING_SWEEP_FAILURE_POLICY_FAIL_FAST = "TUNING_SWEEP_FAILURE_POLICY_FAIL_FAST",
  TUNING_SWEEP_FAILURE_POLICY_WARN_AND_CONTINUE = "TUNING_SWEEP_FAILURE_POLICY_WARN_AND_CONTINUE",
}

/**
 * UnitTag is an operator note for a tagged unit. kind is "module" (key = module
 * ID), "board" (key = MAC address), or "power_feed" (key = container power-feed
 * ID, e.g. pdu-1-outlet-03).
 */
export interface V2UnitTag {
  key?: string;
  kind?: string;
  tag?: string;
  /** @format date-time */
  updatedAt?: string;
  updatedBy?: string;
}

/**
 * UnlockStatus contains the device hardware lock state, independent of any
 * effective secure-policy override.
 */
export interface V2UnlockStatus {
  /** @format date-time */
  expiresAt?: string;
  /**
   * Authoritative R2 hardware lock observation. Producers must explicitly set
   * this when known; absence and observation failures remain UNKNOWN.
   */
  state?: V2LockState;
  /**
   * Deprecated compatibility projection. R2 producers must set this to true
   * exactly when state is LOCK_STATE_UNLOCKED while legacy clients remain.
   */
  unlocked?: boolean;
}

/**
 * Success means the desired intent committed, not that owners have applied it.
 * GetMiningGroupTelemetry reports allocation, application, and pending owners.
 */
export interface V2UpdateMiningGroupIntentResponse {
  group?: V2MiningGroupRecord;
}

export interface V2UpdateMiningGroupResponse {
  group?: V2MiningGroupRecord;
}

export interface V2UpdatePSUFirmwareResponse {
  accepted?: boolean;
  message?: string;
}

export interface V2UpdatePoolResponse {
  /**
   * Pool represents a mining pool configuration.
   * The password field is write-only and never included in responses.
   */
  pool?: V2Pool;
}

/** VoltageRange defines requested PSU voltage sweep points in volts. */
export interface V2VoltageRange {
  /** @format double */
  endVoltage?: number;
  /** @format double */
  startVoltage?: number;
  /** @format double */
  stepVoltage?: number;
}

export interface V2WatchFirmwareUpdateResponse {
  /**
   * Time this snapshot was observed.
   * @format date-time
   */
  observedAt?: string;
  operationId?: string;
  /** Options held by the current server process; defaults after a server restart. */
  options?: V2SystemUpdateOptions;
  /** Existing software-update status, scoped to this operation. */
  status?: V2SoftwareUpdateStatus;
}

export interface V2WiringBoard {
  automaticRecoveryAllowed?: boolean;
  /** @format int64 */
  bridgePort?: number;
  deferredReason?: string;
  evidenceSource?: string;
  expectedFeedId?: string;
  /** @format int64 */
  hashboardPosition?: number;
  interfaceName?: string;
  linkUp?: boolean;
  locationAssigned?: boolean;
  macAddress?: string;
  /** @format int32 */
  module?: number;
  moduleId?: string;
  /** @format date-time */
  nextDue?: string;
  /** @format date-time */
  observedAt?: string;
  observedFeedId?: string;
  portId?: string;
  queued?: boolean;
  reasons?: string[];
  reducedCoverage?: string[];
  /** @format int64 */
  reportedPosition?: number;
  status?: string;
  supersededBy?: string;
  switchId?: string;
  /** @format int32 */
  tank?: number;
  /** @format date-time */
  verifiedAt?: string;
}

export interface V2WiringEvent {
  /** @format date-time */
  at?: string;
  detail?: string;
  /** @format double */
  dropWatts?: number;
  expectedFeedId?: string;
  kind?: string;
  macAddress?: string;
  observedFeedId?: string;
  /** @format double */
  recoveryWatts?: number;
}

export interface V2WiringReport {
  boards?: V2WiringBoard[];
  /**
   * Configuration readiness only: not_configured, partial, or ready. Live
   * switch evidence and HA capability agreement remain independent gates.
   */
  configurationReadiness?: string;
  configurationReasons?: string[];
  deferredReason?: string;
  generation?: string;
  history?: V2WiringEvent[];
  layout?: string;
  mode?: string;
  switches?: V2WiringSwitch[];
  /** @format date-time */
  updatedAt?: string;
}

export interface V2WiringSwitch {
  address?: string;
  engineId?: string;
  error?: string;
  id?: string;
  /** @format date-time */
  observedAt?: string;
}

export interface V2WriteFirmwareArtifactUploadChunkResponse {
  /** @format date-time */
  expiresAt?: string;
  /** @format int64 */
  imageIndex?: number;
  /**
   * next_offset is the number of contiguous bytes received for this image.
   * @format int64
   */
  nextOffset?: string;
  /**
   * received_bytes and total_bytes cover every image in the upload.
   * @format int64
   */
  receivedBytes?: string;
  /** @format int64 */
  totalBytes?: string;
}

export interface V2WritePSUFirmwareCapsuleUploadChunkResponse {
  /** @format date-time */
  expiresAt?: string;
  /**
   * next_offset is the number of contiguous capsule bytes received.
   * @format int64
   */
  nextOffset?: string;
  /** @format int64 */
  totalBytes?: string;
}

export type QueryParamsType = Record<string | number, any>;
export type ResponseFormat = keyof Omit<Body, "body" | "bodyUsed">;

export interface FullRequestParams extends Omit<RequestInit, "body"> {
  /** set parameter to `true` for call `securityWorker` for this request */
  secure?: boolean;
  /** request path */
  path: string;
  /** content type of request body */
  type?: ContentType;
  /** query params */
  query?: QueryParamsType;
  /** format of response (i.e. response.json() -> format: "json") */
  format?: ResponseFormat;
  /** request body */
  body?: unknown;
  /** base url */
  baseUrl?: string;
  /** request cancellation token */
  cancelToken?: CancelToken;
}

export type RequestParams = Omit<
  FullRequestParams,
  "body" | "method" | "query" | "path"
>;

export interface ApiConfig<SecurityDataType = unknown> {
  baseUrl?: string;
  baseApiParams?: Omit<RequestParams, "baseUrl" | "cancelToken" | "signal">;
  securityWorker?: (
    securityData: SecurityDataType | null,
  ) => Promise<RequestParams | void> | RequestParams | void;
  customFetch?: typeof fetch;
}

export interface HttpResponse<D extends unknown, E extends unknown = unknown>
  extends Response {
  data: D;
  error: E;
}

type CancelToken = Symbol | string | number;

export enum ContentType {
  Json = "application/json",
  JsonApi = "application/vnd.api+json",
  FormData = "multipart/form-data",
  UrlEncoded = "application/x-www-form-urlencoded",
  Text = "text/plain",
}

export class HttpClient<SecurityDataType = unknown> {
  public baseUrl: string = "";
  private securityData: SecurityDataType | null = null;
  private securityWorker?: ApiConfig<SecurityDataType>["securityWorker"];
  private abortControllers = new Map<CancelToken, AbortController>();
  private customFetch = (...fetchParams: Parameters<typeof fetch>) =>
    fetch(...fetchParams);

  private baseApiParams: RequestParams = {
    credentials: "same-origin",
    headers: {},
    redirect: "follow",
    referrerPolicy: "no-referrer",
  };

  constructor(apiConfig: ApiConfig<SecurityDataType> = {}) {
    Object.assign(this, apiConfig);
  }

  public setSecurityData = (data: SecurityDataType | null) => {
    this.securityData = data;
  };

  protected encodeQueryParam(key: string, value: any) {
    const encodedKey = encodeURIComponent(key);
    return `${encodedKey}=${encodeURIComponent(typeof value === "number" ? value : `${value}`)}`;
  }

  protected addQueryParam(query: QueryParamsType, key: string) {
    return this.encodeQueryParam(key, query[key]);
  }

  protected addArrayQueryParam(query: QueryParamsType, key: string) {
    const value = query[key];
    return value.map((v: any) => this.encodeQueryParam(key, v)).join("&");
  }

  protected toQueryString(rawQuery?: QueryParamsType): string {
    const query = rawQuery || {};
    const keys = Object.keys(query).filter(
      (key) => "undefined" !== typeof query[key],
    );
    return keys
      .map((key) =>
        Array.isArray(query[key])
          ? this.addArrayQueryParam(query, key)
          : this.addQueryParam(query, key),
      )
      .join("&");
  }

  protected addQueryParams(rawQuery?: QueryParamsType): string {
    const queryString = this.toQueryString(rawQuery);
    return queryString ? `?${queryString}` : "";
  }

  private contentFormatters: Record<ContentType, (input: any) => any> = {
    [ContentType.Json]: (input: any) =>
      input !== null && (typeof input === "object" || typeof input === "string")
        ? JSON.stringify(input)
        : input,
    [ContentType.JsonApi]: (input: any) =>
      input !== null && (typeof input === "object" || typeof input === "string")
        ? JSON.stringify(input)
        : input,
    [ContentType.Text]: (input: any) =>
      input !== null && typeof input !== "string"
        ? JSON.stringify(input)
        : input,
    [ContentType.FormData]: (input: any) => {
      if (input instanceof FormData) {
        return input;
      }

      return Object.keys(input || {}).reduce((formData, key) => {
        const property = input[key];
        formData.append(
          key,
          property instanceof Blob
            ? property
            : typeof property === "object" && property !== null
              ? JSON.stringify(property)
              : `${property}`,
        );
        return formData;
      }, new FormData());
    },
    [ContentType.UrlEncoded]: (input: any) => this.toQueryString(input),
  };

  protected mergeRequestParams(
    params1: RequestParams,
    params2?: RequestParams,
  ): RequestParams {
    return {
      ...this.baseApiParams,
      ...params1,
      ...(params2 || {}),
      headers: {
        ...(this.baseApiParams.headers || {}),
        ...(params1.headers || {}),
        ...((params2 && params2.headers) || {}),
      },
    };
  }

  protected createAbortSignal = (
    cancelToken: CancelToken,
  ): AbortSignal | undefined => {
    if (this.abortControllers.has(cancelToken)) {
      const abortController = this.abortControllers.get(cancelToken);
      if (abortController) {
        return abortController.signal;
      }
      return void 0;
    }

    const abortController = new AbortController();
    this.abortControllers.set(cancelToken, abortController);
    return abortController.signal;
  };

  public abortRequest = (cancelToken: CancelToken) => {
    const abortController = this.abortControllers.get(cancelToken);

    if (abortController) {
      abortController.abort();
      this.abortControllers.delete(cancelToken);
    }
  };

  public request = async <T = any, E = any>({
    body,
    secure,
    path,
    type,
    query,
    format,
    baseUrl,
    cancelToken,
    ...params
  }: FullRequestParams): Promise<HttpResponse<T, E>> => {
    const secureParams =
      ((typeof secure === "boolean" ? secure : this.baseApiParams.secure) &&
        this.securityWorker &&
        (await this.securityWorker(this.securityData))) ||
      {};
    const requestParams = this.mergeRequestParams(params, secureParams);
    const queryString = query && this.toQueryString(query);
    const payloadFormatter = this.contentFormatters[type || ContentType.Json];
    const responseFormat = format || requestParams.format;

    return this.customFetch(
      `${baseUrl || this.baseUrl || ""}${path}${queryString ? `?${queryString}` : ""}`,
      {
        ...requestParams,
        headers: {
          ...(requestParams.headers || {}),
          ...(type && type !== ContentType.FormData
            ? { "Content-Type": type }
            : {}),
        },
        signal:
          (cancelToken
            ? this.createAbortSignal(cancelToken)
            : requestParams.signal) || null,
        body:
          typeof body === "undefined" || body === null
            ? null
            : payloadFormatter(body),
      },
    ).then(async (response) => {
      const r = response as HttpResponse<T, E>;
      r.data = null as unknown as T;
      r.error = null as unknown as E;

      const responseToParse = responseFormat ? response.clone() : response;
      const data = !responseFormat
        ? r
        : await responseToParse[responseFormat]()
            .then((data) => {
              if (r.ok) {
                r.data = data;
              } else {
                r.error = data;
              }
              return r;
            })
            .catch((e) => {
              r.error = e;
              return r;
            });

      if (cancelToken) {
        this.abortControllers.delete(cancelToken);
      }

      if (!response.ok) throw data;
      return data;
    });
  };
}

/**
 * @title Proto MDK v2
 * @version 2-38eb3f68b66035c6
 *
 * Latest MDK v2 REST contract. Device support depends on product and firmware version.
 */
export class Api<
  SecurityDataType extends unknown,
> extends HttpClient<SecurityDataType> {
  api = {
    /**
     * No description
     *
     * @tags Alerts
     * @name AlertServiceListAlerts
     * @summary ListAlerts returns alerts with optional filtering and pagination.
     * @request GET:/api/v2/alerts
     */
    alertServiceListAlerts: (
      query: AlertServiceListAlertsParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2ListAlertsResponse, RpcStatus>({
        path: `/api/v2/alerts`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Alerts
     * @name AlertServiceStreamAlerts
     * @summary StreamAlerts streams new alerts as they fire.
     * @request GET:/api/v2/alerts/stream
     */
    alertServiceStreamAlerts: (
      query: AlertServiceStreamAlertsParams = {},
      params: RequestParams = {},
    ) =>
      this.request<
        {
          /**
           * The `Status` type defines a logical error model that is suitable for
           * different programming environments, including REST APIs and RPC APIs. It is
           * used by [gRPC](https://github.com/grpc). Each `Status` message contains
           * three pieces of data: error code, error message, and error details.
           *
           * You can find out more about this error model and how to work with it in the
           * [API Design Guide](https://cloud.google.com/apis/design/errors).
           */
          error?: RpcStatus;
          result?: V2StreamAlertsResponse;
        },
        RpcStatus
      >({
        path: `/api/v2/alerts/stream`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Alerts
     * @name AlertServiceAcknowledgeAlert
     * @summary AcknowledgeAlert marks an alert as acknowledged.
     * @request POST:/api/v2/alerts/{id}/acknowledge
     */
    alertServiceAcknowledgeAlert: (
      { id }: AlertServiceAcknowledgeAlertParams,
      params: RequestParams = {},
    ) =>
      this.request<V2AcknowledgeAlertResponse, RpcStatus>({
        path: `/api/v2/alerts/${id}/acknowledge`,
        method: "POST",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Auth
     * @name AuthServiceLogin
     * @summary Login authenticates a user and returns a session token.
     * @request POST:/api/v2/auth/login
     */
    authServiceLogin: (body: V2LoginRequest, params: RequestParams = {}) =>
      this.request<V2LoginResponse, RpcStatus>({
        path: `/api/v2/auth/login`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Auth
     * @name AuthServiceLogout
     * @summary Logout invalidates the caller's active session. The REST binding intentionally accepts an empty body and uses the Authorization bearer token, preserving existing callers.
     * @request POST:/api/v2/auth/logout
     */
    authServiceLogout: (
      query: AuthServiceLogoutParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2LogoutResponse, RpcStatus>({
        path: `/api/v2/auth/logout`,
        method: "POST",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Auth
     * @name AuthServiceSetPassword
     * @summary SetPassword sets the initial password during onboarding. The server MUST reject calls once a password has already been set.
     * @request POST:/api/v2/auth/password
     */
    authServiceSetPassword: (
      body: V2SetPasswordRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetPasswordResponse, RpcStatus>({
        path: `/api/v2/auth/password`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Auth
     * @name AuthServiceChangePassword
     * @summary ChangePassword updates the password for an authenticated user.
     * @request PUT:/api/v2/auth/password
     */
    authServiceChangePassword: (
      body: V2ChangePasswordRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2ChangePasswordResponse, RpcStatus>({
        path: `/api/v2/auth/password`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Auth
     * @name AuthServiceRefreshToken
     * @summary RefreshToken exchanges a valid token for a new one.
     * @request POST:/api/v2/auth/refresh
     */
    authServiceRefreshToken: (
      body: V2RefreshTokenRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2RefreshTokenResponse, RpcStatus>({
        path: `/api/v2/auth/refresh`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Auth
     * @name AuthServiceGetAuthStatus
     * @summary GetAuthStatus returns the local authentication-onboarding posture. A client may call it before login to choose initial SetPassword or Login.
     * @request GET:/api/v2/auth/status
     */
    authServiceGetAuthStatus: (params: RequestParams = {}) =>
      this.request<V2GetAuthStatusResponse, RpcStatus>({
        path: `/api/v2/auth/status`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Hashboards, CT1
     * @name HashboardServiceGetContainerMap
     * @summary GetContainerMap returns the full physical layout of a CT1 container.
     * @request GET:/api/v2/container/map
     */
    hashboardServiceGetContainerMap: (params: RequestParams = {}) =>
      this.request<V2GetContainerMapResponse, RpcStatus>({
        path: `/api/v2/container/map`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Diagnostics, CT1
     * @name DiagnosticsServiceDescribeReadOnlyDatabase
     * @summary DescribeReadOnlyDatabase lists the database objects available to the dedicated diagnostics reader role.
     * @request GET:/api/v2/diagnostics/database
     */
    diagnosticsServiceDescribeReadOnlyDatabase: (params: RequestParams = {}) =>
      this.request<V2DescribeReadOnlyDatabaseResponse, RpcStatus>({
        path: `/api/v2/diagnostics/database`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Diagnostics, CT1
     * @name DiagnosticsServiceQueryReadOnlySql
     * @summary QueryReadOnlySQL runs exactly one bounded SELECT statement through the dedicated diagnostics reader role.
     * @request POST:/api/v2/diagnostics/database/query
     */
    diagnosticsServiceQueryReadOnlySql: (
      body: V2QueryReadOnlySQLRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2QueryReadOnlySQLResponse, RpcStatus>({
        path: `/api/v2/diagnostics/database/query`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceGetFeedMappingReport
     * @summary GetFeedMappingReport returns the currently stored feed mapping report.
     * @request GET:/api/v2/feed-mapping
     */
    feedMappingServiceGetFeedMappingReport: (
      query: FeedMappingServiceGetFeedMappingReportParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2GetFeedMappingReportResponse, RpcStatus>({
        path: `/api/v2/feed-mapping`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceGetActiveFeedMappingRun
     * @summary GetActiveFeedMappingRun returns the active feed mapping run, if one is running.
     * @request GET:/api/v2/feed-mapping/active-run
     */
    feedMappingServiceGetActiveFeedMappingRun: (params: RequestParams = {}) =>
      this.request<V2GetActiveFeedMappingRunResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/active-run`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceStartFeedMappingRun
     * @summary StartFeedMappingRun starts a feed mapping run.
     * @request POST:/api/v2/feed-mapping/runs
     */
    feedMappingServiceStartFeedMappingRun: (
      config: V2FeedMappingRunConfig,
      params: RequestParams = {},
    ) =>
      this.request<V2StartFeedMappingRunResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/runs`,
        method: "POST",
        body: config,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceGetFeedMappingRun
     * @summary GetFeedMappingRun returns one feed mapping run snapshot.
     * @request GET:/api/v2/feed-mapping/runs/{runId}
     */
    feedMappingServiceGetFeedMappingRun: (
      { runId }: FeedMappingServiceGetFeedMappingRunParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetFeedMappingRunResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/runs/${runId}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceListFeedMappingRunEvents
     * @summary ListFeedMappingRunEvents returns buffered events for a feed mapping run.
     * @request GET:/api/v2/feed-mapping/runs/{runId}/events
     */
    feedMappingServiceListFeedMappingRunEvents: (
      { runId }: FeedMappingServiceListFeedMappingRunEventsParams,
      params: RequestParams = {},
    ) =>
      this.request<V2ListFeedMappingRunEventsResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/runs/${runId}/events`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceCancelFeedMappingRun
     * @summary CancelFeedMappingRun cancels an active feed mapping run and restores feeds when configured.
     * @request POST:/api/v2/feed-mapping/runs/{runId}:cancel
     */
    feedMappingServiceCancelFeedMappingRun: (
      { runId, cancel }: FeedMappingServiceCancelFeedMappingRunParams,
      body: FeedMappingServiceCancelFeedMappingRunBody,
      params: RequestParams = {},
    ) =>
      this.request<V2CancelFeedMappingRunResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/runs/${runId}${cancel}`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceGetActiveSoftFeedMappingRun
     * @summary GetActiveSoftFeedMappingRun returns the active soft mapping run, if one is running.
     * @request GET:/api/v2/feed-mapping/soft-active-run
     */
    feedMappingServiceGetActiveSoftFeedMappingRun: (
      params: RequestParams = {},
    ) =>
      this.request<V2GetActiveSoftFeedMappingRunResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/soft-active-run`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceGetSoftFeedMappingReport
     * @summary GetSoftFeedMappingReport returns the most recent soft mapping run, if one exists.
     * @request GET:/api/v2/feed-mapping/soft-report
     */
    feedMappingServiceGetSoftFeedMappingReport: (params: RequestParams = {}) =>
      this.request<V2GetSoftFeedMappingReportResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/soft-report`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceStartSoftFeedMappingRun
     * @summary StartSoftFeedMappingRun verifies board-to-feed mappings by perturbing one mining board at a time.
     * @request POST:/api/v2/feed-mapping/soft-runs
     */
    feedMappingServiceStartSoftFeedMappingRun: (
      config: V2SoftFeedMappingRunConfig,
      params: RequestParams = {},
    ) =>
      this.request<V2StartSoftFeedMappingRunResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/soft-runs`,
        method: "POST",
        body: config,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceGetSoftFeedMappingRun
     * @summary GetSoftFeedMappingRun returns one soft mapping run snapshot.
     * @request GET:/api/v2/feed-mapping/soft-runs/{runId}
     */
    feedMappingServiceGetSoftFeedMappingRun: (
      { runId }: FeedMappingServiceGetSoftFeedMappingRunParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetSoftFeedMappingRunResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/soft-runs/${runId}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceApplySoftFeedMappingRun
     * @summary ApplySoftFeedMappingRun applies high-confidence changed/added mappings from a completed soft run.
     * @request POST:/api/v2/feed-mapping/soft-runs/{runId}:apply
     */
    feedMappingServiceApplySoftFeedMappingRun: (
      { runId, apply }: FeedMappingServiceApplySoftFeedMappingRunParams,
      body: FeedMappingServiceApplySoftFeedMappingRunBody,
      params: RequestParams = {},
    ) =>
      this.request<V2ApplySoftFeedMappingRunResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/soft-runs/${runId}${apply}`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceCancelSoftFeedMappingRun
     * @summary CancelSoftFeedMappingRun cancels an active soft mapping run and restores the current board when possible.
     * @request POST:/api/v2/feed-mapping/soft-runs/{runId}:cancel
     */
    feedMappingServiceCancelSoftFeedMappingRun: (
      { runId, cancel }: FeedMappingServiceCancelSoftFeedMappingRunParams,
      body: FeedMappingServiceCancelSoftFeedMappingRunBody,
      params: RequestParams = {},
    ) =>
      this.request<V2CancelSoftFeedMappingRunResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/soft-runs/${runId}${cancel}`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServiceRequestWiringVerification
     * @summary RequestWiringVerification queues checks; it never rewrites installation positions.
     * @request POST:/api/v2/feed-mapping/wiring:verify
     */
    feedMappingServiceRequestWiringVerification: (
      { verify }: FeedMappingServiceRequestWiringVerificationParams,
      body: V2RequestWiringVerificationRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2RequestWiringVerificationResponse, RpcStatus>({
        path: `/api/v2/feed-mapping/wiring${verify}`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Feed Mapping, CT1
     * @name FeedMappingServicePlanFeedMappingRun
     * @summary PlanFeedMappingRun validates and previews a feed mapping run.
     * @request POST:/api/v2/feed-mapping:plan
     */
    feedMappingServicePlanFeedMappingRun: (
      { plan }: FeedMappingServicePlanFeedMappingRunParams,
      config: V2FeedMappingRunConfig,
      params: RequestParams = {},
    ) =>
      this.request<V2PlanFeedMappingRunResponse, RpcStatus>({
        path: `/api/v2/feed-mapping${plan}`,
        method: "POST",
        body: config,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware, CT1
     * @name FirmwareServiceGetFirmwarePolicy
     * @summary GetFirmwarePolicy returns the container's desired exact firmware target.
     * @request GET:/api/v2/firmware/policy
     */
    firmwareServiceGetFirmwarePolicy: (params: RequestParams = {}) =>
      this.request<V2GetFirmwarePolicyResponse, RpcStatus>({
        path: `/api/v2/firmware/policy`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware, CT1
     * @name FirmwareServiceSetFirmwarePolicy
     * @summary SetFirmwarePolicy revision-fences a hot policy replacement or disable.
     * @request PUT:/api/v2/firmware/policy
     */
    firmwareServiceSetFirmwarePolicy: (
      body: V2SetFirmwarePolicyRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetFirmwarePolicyResponse, RpcStatus>({
        path: `/api/v2/firmware/policy`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware, CT1
     * @name FirmwareServiceListFirmwarePolicyBoards
     * @summary ListFirmwarePolicyBoards returns retained compliance and retry state.
     * @request GET:/api/v2/firmware/policy/boards
     */
    firmwareServiceListFirmwarePolicyBoards: (params: RequestParams = {}) =>
      this.request<V2ListFirmwarePolicyBoardsResponse, RpcStatus>({
        path: `/api/v2/firmware/policy/boards`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware, CT1
     * @name FirmwareServiceRetryFirmwarePolicyBoards
     * @summary RetryFirmwarePolicyBoards resets terminal policy failures for a scope.
     * @request POST:/api/v2/firmware/policy/retry
     */
    firmwareServiceRetryFirmwarePolicyBoards: (
      body: V2RetryFirmwarePolicyBoardsRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2RetryFirmwarePolicyBoardsResponse, RpcStatus>({
        path: `/api/v2/firmware/policy/retry`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware
     * @name FirmwareServiceListFirmwareUpdates
     * @summary ListFirmwareUpdates lists active and completed update jobs, newest first.
     * @request GET:/api/v2/firmware/updates
     */
    firmwareServiceListFirmwareUpdates: (
      query: FirmwareServiceListFirmwareUpdatesParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2ListFirmwareUpdatesResponse, RpcStatus>({
        path: `/api/v2/firmware/updates`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware
     * @name FirmwareServiceStartFirmwareUpdate
     * @summary StartFirmwareUpdate deploys an uploaded firmware artifact to the selected boards.
     * @request POST:/api/v2/firmware/updates
     */
    firmwareServiceStartFirmwareUpdate: (
      body: V2StartFirmwareUpdateRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2StartFirmwareUpdateResponse, RpcStatus>({
        path: `/api/v2/firmware/updates`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware
     * @name FirmwareServiceGetFirmwareUpdate
     * @summary GetFirmwareUpdate returns the current state of one update job.
     * @request GET:/api/v2/firmware/updates/{taskId}
     */
    firmwareServiceGetFirmwareUpdate: (
      { taskId, ...query }: FirmwareServiceGetFirmwareUpdateParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetFirmwareUpdateResponse, RpcStatus>({
        path: `/api/v2/firmware/updates/${taskId}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware
     * @name FirmwareServiceCancelFirmwareUpdate
     * @summary CancelFirmwareUpdate requests cancellation of an active update job.
     * @request POST:/api/v2/firmware/updates/{taskId}/cancel
     */
    firmwareServiceCancelFirmwareUpdate: (
      { taskId, ...query }: FirmwareServiceCancelFirmwareUpdateParams,
      params: RequestParams = {},
    ) =>
      this.request<V2CancelFirmwareUpdateResponse, RpcStatus>({
        path: `/api/v2/firmware/updates/${taskId}/cancel`,
        method: "POST",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware, CT1
     * @name FirmwareServiceBeginFirmwareArtifactUpload
     * @summary BeginFirmwareArtifactUpload starts a chunked unary upload for clients that cannot send client-streaming calls, such as browsers using the Connect or REST gateways. It validates the same metadata as UploadFirmwareArtifact and returns an upload ID that exists only on the connected node. Idle uploads expire and release their staged bytes.
     * @request POST:/api/v2/firmware/uploads
     */
    firmwareServiceBeginFirmwareArtifactUpload: (
      body: V2BeginFirmwareArtifactUploadRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2BeginFirmwareArtifactUploadResponse, RpcStatus>({
        path: `/api/v2/firmware/uploads`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware, CT1
     * @name FirmwareServiceAbortFirmwareArtifactUpload
     * @summary AbortFirmwareArtifactUpload discards an unfinished upload. Aborting an unknown or expired upload succeeds.
     * @request DELETE:/api/v2/firmware/uploads/{uploadId}
     */
    firmwareServiceAbortFirmwareArtifactUpload: (
      { uploadId }: FirmwareServiceAbortFirmwareArtifactUploadParams,
      params: RequestParams = {},
    ) =>
      this.request<V2AbortFirmwareArtifactUploadResponse, RpcStatus>({
        path: `/api/v2/firmware/uploads/${uploadId}`,
        method: "DELETE",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware, CT1
     * @name FirmwareServiceWriteFirmwareArtifactUploadChunk
     * @summary WriteFirmwareArtifactUploadChunk writes bytes for one image at an exact offset. Images may be written in any order, but each image's bytes must be contiguous. Repeating an already received chunk is accepted, so a client can safely retry after a lost response.
     * @request POST:/api/v2/firmware/uploads/{uploadId}/chunks
     */
    firmwareServiceWriteFirmwareArtifactUploadChunk: (
      { uploadId }: FirmwareServiceWriteFirmwareArtifactUploadChunkParams,
      body: FirmwareServiceWriteFirmwareArtifactUploadChunkBody,
      params: RequestParams = {},
    ) =>
      this.request<V2WriteFirmwareArtifactUploadChunkResponse, RpcStatus>({
        path: `/api/v2/firmware/uploads/${uploadId}/chunks`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Firmware, CT1
     * @name FirmwareServiceCommitFirmwareArtifactUpload
     * @summary CommitFirmwareArtifactUpload validates and caches a complete upload, exactly like the final step of UploadFirmwareArtifact. An incomplete upload is refused without discarding received bytes.
     * @request POST:/api/v2/firmware/uploads/{uploadId}/commit
     */
    firmwareServiceCommitFirmwareArtifactUpload: (
      { uploadId }: FirmwareServiceCommitFirmwareArtifactUploadParams,
      params: RequestParams = {},
    ) =>
      this.request<V2CommitFirmwareArtifactUploadResponse, RpcStatus>({
        path: `/api/v2/firmware/uploads/${uploadId}/commit`,
        method: "POST",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Hashboards
     * @name HashboardServiceListHashboards
     * @summary ListHashboards returns currently visible hashboards with their latest health snapshots.
     * @request GET:/api/v2/hashboards
     */
    hashboardServiceListHashboards: (params: RequestParams = {}) =>
      this.request<V2ListHashboardsResponse, RpcStatus>({
        path: `/api/v2/hashboards`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Hashboards, CT1
     * @name HashboardServiceSetIdentifyLed
     * @summary SetIdentifyLED records the desired identification LED state for a board or module. The HA node that owns each board applies the command to firmware.
     * @request POST:/api/v2/hashboards/identify-led
     */
    hashboardServiceSetIdentifyLed: (
      body: V2SetIdentifyLEDRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetIdentifyLEDResponse, RpcStatus>({
        path: `/api/v2/hashboards/identify-led`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Hashboards
     * @name HashboardServiceGetHashboard
     * @summary GetHashboard returns one hashboard selected by CT1 MAC address or R2 serial number.
     * @request GET:/api/v2/hashboards/{identifier}
     */
    hashboardServiceGetHashboard: (
      { identifier }: HashboardServiceGetHashboardParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetHashboardResponse, RpcStatus>({
        path: `/api/v2/hashboards/${identifier}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Hashboards
     * @name HashboardServiceGetHashboardAsicMetadata
     * @summary GetHashboardASICMetadata reads decoded ASIC eFuse metadata from a connected board selected by CT1 MAC address or R2 serial number. Firmware makes this data available only after ASIC enumeration, so the board must have reached the mining state. An implementation may cache a successful read for the lifetime of that board connection.
     * @request GET:/api/v2/hashboards/{identifier}/asic-metadata
     */
    hashboardServiceGetHashboardAsicMetadata: (
      { identifier }: HashboardServiceGetHashboardAsicMetadataParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetHashboardASICMetadataResponse, RpcStatus>({
        path: `/api/v2/hashboards/${identifier}/asic-metadata`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Hashboards
     * @name HashboardServiceGetHashboardAsiCs
     * @summary GetHashboardASICs returns ASIC-level health data for one hashboard selected by CT1 MAC address or R2 serial number.
     * @request GET:/api/v2/hashboards/{identifier}/asics
     */
    hashboardServiceGetHashboardAsiCs: (
      { identifier }: HashboardServiceGetHashboardAsiCsParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetHashboardASICsResponse, RpcStatus>({
        path: `/api/v2/hashboards/${identifier}/asics`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Hashboards
     * @name HashboardServiceGetHashboardEvents
     * @summary GetHashboardEvents returns recent history for one hashboard selected by CT1 MAC address or R2 serial number.
     * @request GET:/api/v2/hashboards/{identifier}/events
     */
    hashboardServiceGetHashboardEvents: (
      { identifier, ...query }: HashboardServiceGetHashboardEventsParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetHashboardEventsResponse, RpcStatus>({
        path: `/api/v2/hashboards/${identifier}/events`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Run-In, CT1
     * @name RunInServiceStartHashboardRunIn
     * @summary StartHashboardRunIn starts a factory run-in task for one hashboard.
     * @request POST:/api/v2/hashboards/{identifier}/run-in
     */
    runInServiceStartHashboardRunIn: (
      { identifier, ...query }: RunInServiceStartHashboardRunInParams,
      config: V2HashboardRunInConfig,
      params: RequestParams = {},
    ) =>
      this.request<V2StartHashboardRunInResponse, RpcStatus>({
        path: `/api/v2/hashboards/${identifier}/run-in`,
        method: "POST",
        query: query,
        body: config,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Manufacturing, CT1
     * @name ManufacturingServiceListManufacturingBoards
     * @request GET:/api/v2/manufacturing/boards
     */
    manufacturingServiceListManufacturingBoards: (params: RequestParams = {}) =>
      this.request<V2ListManufacturingBoardsResponse, RpcStatus>({
        path: `/api/v2/manufacturing/boards`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Manufacturing, CT1
     * @name ManufacturingServiceAdoptManufacturingBoard
     * @request POST:/api/v2/manufacturing/boards/{identifier}/adopt
     */
    manufacturingServiceAdoptManufacturingBoard: (
      { identifier }: ManufacturingServiceAdoptManufacturingBoardParams,
      body: ManufacturingServiceAdoptManufacturingBoardBody,
      params: RequestParams = {},
    ) =>
      this.request<V2AdoptManufacturingBoardResponse, RpcStatus>({
        path: `/api/v2/manufacturing/boards/${identifier}/adopt`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Manufacturing, CT1
     * @name ManufacturingServiceListManufacturingSelfTests
     * @request GET:/api/v2/manufacturing/self-tests
     */
    manufacturingServiceListManufacturingSelfTests: (
      query: ManufacturingServiceListManufacturingSelfTestsParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2ListManufacturingSelfTestsResponse, RpcStatus>({
        path: `/api/v2/manufacturing/self-tests`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Manufacturing, CT1
     * @name ManufacturingServiceStartManufacturingSelfTest
     * @request POST:/api/v2/manufacturing/self-tests
     */
    manufacturingServiceStartManufacturingSelfTest: (
      body: V2StartManufacturingSelfTestRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2StartManufacturingSelfTestResponse, RpcStatus>({
        path: `/api/v2/manufacturing/self-tests`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Manufacturing, CT1
     * @name ManufacturingServiceGetManufacturingSelfTest
     * @request GET:/api/v2/manufacturing/self-tests/{testId}
     */
    manufacturingServiceGetManufacturingSelfTest: (
      { testId }: ManufacturingServiceGetManufacturingSelfTestParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetManufacturingSelfTestResponse, RpcStatus>({
        path: `/api/v2/manufacturing/self-tests/${testId}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceListMiningGroups
     * @request GET:/api/v2/mining-groups
     */
    miningGroupServiceListMiningGroups: (params: RequestParams = {}) =>
      this.request<V2ListMiningGroupsResponse, RpcStatus>({
        path: `/api/v2/mining-groups`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceCreateMiningGroup
     * @request POST:/api/v2/mining-groups
     */
    miningGroupServiceCreateMiningGroup: (
      body: V2CreateMiningGroupRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2CreateMiningGroupResponse, RpcStatus>({
        path: `/api/v2/mining-groups`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceActivateMiningGroups
     * @request POST:/api/v2/mining-groups/activate
     */
    miningGroupServiceActivateMiningGroups: (
      body: V2ActivateMiningGroupsRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2ActivateMiningGroupsResponse, RpcStatus>({
        path: `/api/v2/mining-groups/activate`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceQueryMiningGroupHistory
     * @request GET:/api/v2/mining-groups/by-id/{groupId}/history
     */
    miningGroupServiceQueryMiningGroupHistory: (
      { groupId, ...query }: MiningGroupServiceQueryMiningGroupHistoryParams,
      params: RequestParams = {},
    ) =>
      this.request<V2QueryMiningGroupHistoryResponse, RpcStatus>({
        path: `/api/v2/mining-groups/by-id/${groupId}/history`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceGetMiningGroupPoolDiagnostics
     * @request GET:/api/v2/mining-groups/by-id/{groupId}/pool-diagnostics
     */
    miningGroupServiceGetMiningGroupPoolDiagnostics: (
      { groupId }: MiningGroupServiceGetMiningGroupPoolDiagnosticsParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetMiningGroupPoolDiagnosticsResponse, RpcStatus>({
        path: `/api/v2/mining-groups/by-id/${groupId}/pool-diagnostics`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceListMiningGroupPools
     * @request GET:/api/v2/mining-groups/by-id/{groupId}/pools
     */
    miningGroupServiceListMiningGroupPools: (
      { groupId }: MiningGroupServiceListMiningGroupPoolsParams,
      params: RequestParams = {},
    ) =>
      this.request<V2ListMiningGroupPoolsResponse, RpcStatus>({
        path: `/api/v2/mining-groups/by-id/${groupId}/pools`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceReplaceMiningGroupPools
     * @request PUT:/api/v2/mining-groups/by-id/{groupId}/pools
     */
    miningGroupServiceReplaceMiningGroupPools: (
      { groupId }: MiningGroupServiceReplaceMiningGroupPoolsParams,
      body: MiningGroupServiceReplaceMiningGroupPoolsBody,
      params: RequestParams = {},
    ) =>
      this.request<V2ReplaceMiningGroupPoolsResponse, RpcStatus>({
        path: `/api/v2/mining-groups/by-id/${groupId}/pools`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceTestMiningGroupPoolConnection
     * @request POST:/api/v2/mining-groups/by-id/{groupId}/pools/{poolId}/test
     */
    miningGroupServiceTestMiningGroupPoolConnection: (
      {
        groupId,
        poolId,
        ...query
      }: MiningGroupServiceTestMiningGroupPoolConnectionParams,
      params: RequestParams = {},
    ) =>
      this.request<V2TestMiningGroupPoolConnectionResponse, RpcStatus>({
        path: `/api/v2/mining-groups/by-id/${groupId}/pools/${poolId}/test`,
        method: "POST",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceGetMiningGroupTelemetry
     * @request GET:/api/v2/mining-groups/by-id/{groupId}/telemetry
     */
    miningGroupServiceGetMiningGroupTelemetry: (
      { groupId, ...query }: MiningGroupServiceGetMiningGroupTelemetryParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetMiningGroupTelemetryResponse, RpcStatus>({
        path: `/api/v2/mining-groups/by-id/${groupId}/telemetry`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceDeleteMiningGroup
     * @request DELETE:/api/v2/mining-groups/by-id/{id}
     */
    miningGroupServiceDeleteMiningGroup: (
      { id, ...query }: MiningGroupServiceDeleteMiningGroupParams,
      params: RequestParams = {},
    ) =>
      this.request<V2DeleteMiningGroupResponse, RpcStatus>({
        path: `/api/v2/mining-groups/by-id/${id}`,
        method: "DELETE",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceGetMiningGroup
     * @request GET:/api/v2/mining-groups/by-id/{id}
     */
    miningGroupServiceGetMiningGroup: (
      { id }: MiningGroupServiceGetMiningGroupParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetMiningGroupResponse, RpcStatus>({
        path: `/api/v2/mining-groups/by-id/${id}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceUpdateMiningGroup
     * @request PUT:/api/v2/mining-groups/by-id/{id}
     */
    miningGroupServiceUpdateMiningGroup: (
      { id }: MiningGroupServiceUpdateMiningGroupParams,
      body: MiningGroupServiceUpdateMiningGroupBody,
      params: RequestParams = {},
    ) =>
      this.request<V2UpdateMiningGroupResponse, RpcStatus>({
        path: `/api/v2/mining-groups/by-id/${id}`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceUpdateMiningGroupIntent
     * @summary Operator-only, presence-safe intent patch.
     * @request PATCH:/api/v2/mining-groups/by-id/{id}/intent
     */
    miningGroupServiceUpdateMiningGroupIntent: (
      { id }: MiningGroupServiceUpdateMiningGroupIntentParams,
      body: MiningGroupServiceUpdateMiningGroupIntentBody,
      params: RequestParams = {},
    ) =>
      this.request<V2UpdateMiningGroupIntentResponse, RpcStatus>({
        path: `/api/v2/mining-groups/by-id/${id}/intent`,
        method: "PATCH",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceGetMiningGroupCapability
     * @request GET:/api/v2/mining-groups/capability
     */
    miningGroupServiceGetMiningGroupCapability: (params: RequestParams = {}) =>
      this.request<V2GetMiningGroupCapabilityResponse, RpcStatus>({
        path: `/api/v2/mining-groups/capability`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceGetPoolConnectionSharingState
     * @request GET:/api/v2/mining-groups/connection-sharing
     */
    miningGroupServiceGetPoolConnectionSharingState: (
      params: RequestParams = {},
    ) =>
      this.request<V2GetPoolConnectionSharingStateResponse, RpcStatus>({
        path: `/api/v2/mining-groups/connection-sharing`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceSetPoolConnectionSharingEnabled
     * @request PUT:/api/v2/mining-groups/connection-sharing
     */
    miningGroupServiceSetPoolConnectionSharingEnabled: (
      body: V2SetPoolConnectionSharingEnabledRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetPoolConnectionSharingEnabledResponse, RpcStatus>({
        path: `/api/v2/mining-groups/connection-sharing`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceRetryPoolConnectionSharingProfile
     * @request POST:/api/v2/mining-groups/connection-sharing/retry
     */
    miningGroupServiceRetryPoolConnectionSharingProfile: (
      body: V2RetryPoolConnectionSharingProfileRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2RetryPoolConnectionSharingProfileResponse, RpcStatus>({
        path: `/api/v2/mining-groups/connection-sharing/retry`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceSetContainerMiningSuspended
     * @request PUT:/api/v2/mining-groups/container-suspend
     */
    miningGroupServiceSetContainerMiningSuspended: (
      body: V2SetContainerMiningSuspendedRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetContainerMiningSuspendedResponse, RpcStatus>({
        path: `/api/v2/mining-groups/container-suspend`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceDeactivateMiningGroups
     * @request POST:/api/v2/mining-groups/deactivate
     */
    miningGroupServiceDeactivateMiningGroups: (
      body: V2DeactivateMiningGroupsRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2DeactivateMiningGroupsResponse, RpcStatus>({
        path: `/api/v2/mining-groups/deactivate`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceListMiningGroupMemberships
     * @request GET:/api/v2/mining-groups/memberships
     */
    miningGroupServiceListMiningGroupMemberships: (
      query: MiningGroupServiceListMiningGroupMembershipsParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2ListMiningGroupMembershipsResponse, RpcStatus>({
        path: `/api/v2/mining-groups/memberships`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceGetMiningGroupOperation
     * @request GET:/api/v2/mining-groups/operations/{id}
     */
    miningGroupServiceGetMiningGroupOperation: (
      { id }: MiningGroupServiceGetMiningGroupOperationParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetMiningGroupOperationResponse, RpcStatus>({
        path: `/api/v2/mining-groups/operations/${id}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining groups, CT1
     * @name MiningGroupServiceRequestMiningGroupTransfer
     * @request POST:/api/v2/mining-groups/transfers
     */
    miningGroupServiceRequestMiningGroupTransfer: (
      body: V2RequestMiningGroupTransferRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2RequestMiningGroupTransferResponse, RpcStatus>({
        path: `/api/v2/mining-groups/transfers`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining, CT1
     * @name MiningServiceListIsolation
     * @summary ListIsolation returns the current set of isolated hashboards.
     * @request GET:/api/v2/mining/isolation
     */
    miningServiceListIsolation: (params: RequestParams = {}) =>
      this.request<V2ListIsolationResponse, RpcStatus>({
        path: `/api/v2/mining/isolation`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining, CT1
     * @name MiningServiceSetIsolation
     * @summary SetIsolation marks or clears isolation for an explicit set of hashboards. Isolated boards are excluded from cluster start/stop and from the mining-intent reconciler. On clear, the server re-aligns the boards to the current cluster mining intent.
     * @request POST:/api/v2/mining/isolation
     */
    miningServiceSetIsolation: (
      body: V2SetIsolationRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetIsolationResponse, RpcStatus>({
        path: `/api/v2/mining/isolation`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining, CT1
     * @name MiningServiceListModuleVoltageSettings
     * @summary ListModuleVoltageSettings returns durable fixed/AUTO settings and the firmware-supported voltage range for every known module ID.
     * @request GET:/api/v2/mining/modules/voltage
     */
    miningServiceListModuleVoltageSettings: (params: RequestParams = {}) =>
      this.request<V2ListModuleVoltageSettingsResponse, RpcStatus>({
        path: `/api/v2/mining/modules/voltage`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining, CT1
     * @name MiningServiceClearModuleFixedVoltage
     * @summary ClearModuleFixedVoltage persists and applies automatic voltage tuning for one module.
     * @request DELETE:/api/v2/mining/modules/{moduleId}/voltage/fixed
     */
    miningServiceClearModuleFixedVoltage: (
      { moduleId }: MiningServiceClearModuleFixedVoltageParams,
      params: RequestParams = {},
    ) =>
      this.request<V2ClearModuleFixedVoltageResponse, RpcStatus>({
        path: `/api/v2/mining/modules/${moduleId}/voltage/fixed`,
        method: "DELETE",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining, CT1
     * @name MiningServiceSetModuleFixedVoltage
     * @summary SetModuleFixedVoltage persists and applies one module-wide fixed voltage.
     * @request PUT:/api/v2/mining/modules/{moduleId}/voltage/fixed
     */
    miningServiceSetModuleFixedVoltage: (
      { moduleId }: MiningServiceSetModuleFixedVoltageParams,
      body: MiningServiceSetModuleFixedVoltageBody,
      params: RequestParams = {},
    ) =>
      this.request<V2SetModuleFixedVoltageResponse, RpcStatus>({
        path: `/api/v2/mining/modules/${moduleId}/voltage/fixed`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining, CT1
     * @name MiningServiceSetPowerTargetScoped
     * @summary SetPowerTargetScoped sets a manual per-board power target on an explicit set of ISOLATED hashboards. Isolation is required so the fleet power reconciler does not fight the manual target; it is validated per board.
     * @request POST:/api/v2/mining/scoped/power
     */
    miningServiceSetPowerTargetScoped: (
      body: V2SetPowerTargetScopedRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetPowerTargetScopedResponse, RpcStatus>({
        path: `/api/v2/mining/scoped/power`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining, CT1
     * @name MiningServiceClearPowerTargetScoped
     * @summary ClearPowerTargetScoped removes manual per-board power targets from an explicit set of isolated hashboards, restoring automatic tuning.
     * @request POST:/api/v2/mining/scoped/power/clear
     */
    miningServiceClearPowerTargetScoped: (
      body: V2ClearPowerTargetScopedRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2ClearPowerTargetScopedResponse, RpcStatus>({
        path: `/api/v2/mining/scoped/power/clear`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining, CT1
     * @name MiningServiceStartMiningScoped
     * @summary StartMiningScoped begins mining for an explicit set of hashboards. Isolated boards are eligible (this is how operators drive them). Cluster intent is not changed.
     * @request POST:/api/v2/mining/scoped/start
     */
    miningServiceStartMiningScoped: (
      body: V2StartMiningScopedRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2StartMiningScopedResponse, RpcStatus>({
        path: `/api/v2/mining/scoped/start`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining, CT1
     * @name MiningServiceStopMiningScoped
     * @summary StopMiningScoped halts mining for an explicit set of hashboards. Cluster intent is not changed.
     * @request POST:/api/v2/mining/scoped/stop
     */
    miningServiceStopMiningScoped: (
      body: V2StopMiningScopedRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2StopMiningScopedResponse, RpcStatus>({
        path: `/api/v2/mining/scoped/stop`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining
     * @name MiningServiceStartMining
     * @summary StartMining begins mining operations.
     * @request POST:/api/v2/mining/start
     */
    miningServiceStartMining: (params: RequestParams = {}) =>
      this.request<V2StartMiningResponse, RpcStatus>({
        path: `/api/v2/mining/start`,
        method: "POST",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining
     * @name MiningServiceGetMiningStatus
     * @summary GetMiningStatus returns aggregate mining status across all hashboards.
     * @request GET:/api/v2/mining/status
     */
    miningServiceGetMiningStatus: (params: RequestParams = {}) =>
      this.request<V2GetMiningStatusResponse, RpcStatus>({
        path: `/api/v2/mining/status`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining
     * @name MiningServiceStopMining
     * @summary StopMining halts mining operations.
     * @request POST:/api/v2/mining/stop
     */
    miningServiceStopMining: (params: RequestParams = {}) =>
      this.request<V2StopMiningResponse, RpcStatus>({
        path: `/api/v2/mining/stop`,
        method: "POST",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining
     * @name MiningServiceGetTargetPower
     * @summary GetTargetPower returns the current target power setting.
     * @request GET:/api/v2/mining/target-power
     */
    miningServiceGetTargetPower: (params: RequestParams = {}) =>
      this.request<V2GetTargetPowerResponse, RpcStatus>({
        path: `/api/v2/mining/target-power`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining
     * @name MiningServiceSetTargetPower
     * @summary SetTargetPower sets the target power for mining operations.
     * @request PUT:/api/v2/mining/target-power
     */
    miningServiceSetTargetPower: (
      body: V2SetTargetPowerRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetTargetPowerResponse, RpcStatus>({
        path: `/api/v2/mining/target-power`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining
     * @name MiningServiceGetTuning
     * @summary GetTuning returns the active product-specific tuning algorithm.
     * @request GET:/api/v2/mining/tuning
     */
    miningServiceGetTuning: (params: RequestParams = {}) =>
      this.request<V2GetTuningResponse, RpcStatus>({
        path: `/api/v2/mining/tuning`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining
     * @name MiningServiceSetTuning
     * @summary SetTuning configures the exact product-specific tuning algorithm.
     * @request PUT:/api/v2/mining/tuning
     */
    miningServiceSetTuning: (
      body: V2SetTuningRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetTuningResponse, RpcStatus>({
        path: `/api/v2/mining/tuning`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining
     * @name MiningServiceEnableVoltageTuning
     * @summary EnableVoltageTuning enables automatic PSU voltage tuning on local hashboards.
     * @request POST:/api/v2/mining/voltage-tuning/enable
     */
    miningServiceEnableVoltageTuning: (params: RequestParams = {}) =>
      this.request<V2EnableVoltageTuningResponse, RpcStatus>({
        path: `/api/v2/mining/voltage-tuning/enable`,
        method: "POST",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Mining
     * @name MiningServiceSetFixedVoltage
     * @summary SetFixedVoltage disables automatic PSU voltage tuning and requests one fleet-wide fixed voltage from 26,000 through 30,000 millivolts inclusive.
     * @request POST:/api/v2/mining/voltage/fixed
     */
    miningServiceSetFixedVoltage: (
      body: V2SetFixedVoltageRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetFixedVoltageResponse, RpcStatus>({
        path: `/api/v2/mining/voltage/fixed`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Run-In, CT1
     * @name RunInServiceStartModuleRunIn
     * @summary StartModuleRunIn starts one synchronized tuning sweep across the connected hashboards in a PSU module.
     * @request POST:/api/v2/modules/{moduleId}/run-in
     */
    runInServiceStartModuleRunIn: (
      { moduleId, ...query }: RunInServiceStartModuleRunInParams,
      config: V2HashboardRunInConfig,
      params: RequestParams = {},
    ) =>
      this.request<V2StartModuleRunInResponse, RpcStatus>({
        path: `/api/v2/modules/${moduleId}/run-in`,
        method: "POST",
        query: query,
        body: config,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Operational policy, CT1
     * @name OperationalPolicyServiceGetManagedOperationalPolicy
     * @request GET:/api/v2/operational-policy
     */
    operationalPolicyServiceGetManagedOperationalPolicy: (
      params: RequestParams = {},
    ) =>
      this.request<V2GetManagedOperationalPolicyResponse, RpcStatus>({
        path: `/api/v2/operational-policy`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Operational policy, CT1
     * @name OperationalPolicyServiceApplyManagedOperationalPolicy
     * @request PUT:/api/v2/operational-policy
     */
    operationalPolicyServiceApplyManagedOperationalPolicy: (
      body: V2ApplyManagedOperationalPolicyRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2ApplyManagedOperationalPolicyResponse, RpcStatus>({
        path: `/api/v2/operational-policy`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Pools
     * @name PoolServiceListPools
     * @summary ListPools returns all configured mining pools ordered by priority.
     * @request GET:/api/v2/pools
     */
    poolServiceListPools: (params: RequestParams = {}) =>
      this.request<V2ListPoolsResponse, RpcStatus>({
        path: `/api/v2/pools`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Pools
     * @name PoolServiceCreatePool
     * @summary CreatePool adds a new pool configuration.
     * @request POST:/api/v2/pools
     */
    poolServiceCreatePool: (
      body: V2CreatePoolRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2CreatePoolResponse, RpcStatus>({
        path: `/api/v2/pools`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Pools, CT1
     * @name PoolServiceSetPoolAllocationMode
     * @summary SetPoolAllocationMode changes the container's pool allocation strategy in one shared-database transaction. The current strategies are priority failover and weighted time share.
     * @request PUT:/api/v2/pools/allocation-mode
     */
    poolServiceSetPoolAllocationMode: (
      body: V2SetPoolAllocationModeRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetPoolAllocationModeResponse, RpcStatus>({
        path: `/api/v2/pools/allocation-mode`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Pools
     * @name PoolServiceGetPoolDiagnostics
     * @summary GetPoolDiagnostics returns per-configured-enabled-pool runtime connection diagnostics reflecting this node's live pool connections. The manager tracks only enabled pools (SetPools filters to enabled), so disabled pools are not included.
     * @request GET:/api/v2/pools/diagnostics
     */
    poolServiceGetPoolDiagnostics: (
      query: PoolServiceGetPoolDiagnosticsParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2GetPoolDiagnosticsResponse, RpcStatus>({
        path: `/api/v2/pools/diagnostics`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Pools
     * @name PoolServiceDeletePool
     * @summary DeletePool removes a pool configuration.
     * @request DELETE:/api/v2/pools/{id}
     */
    poolServiceDeletePool: (
      { id }: PoolServiceDeletePoolParams,
      params: RequestParams = {},
    ) =>
      this.request<V2DeletePoolResponse, RpcStatus>({
        path: `/api/v2/pools/${id}`,
        method: "DELETE",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Pools
     * @name PoolServiceGetPool
     * @summary GetPool returns a single pool by ID.
     * @request GET:/api/v2/pools/{id}
     */
    poolServiceGetPool: (
      { id }: PoolServiceGetPoolParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetPoolResponse, RpcStatus>({
        path: `/api/v2/pools/${id}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Pools
     * @name PoolServiceTestPoolConnection
     * @summary TestPoolConnection tests connectivity to a configured pool.
     * @request POST:/api/v2/pools/{id}/test
     */
    poolServiceTestPoolConnection: (
      { id }: PoolServiceTestPoolConnectionParams,
      params: RequestParams = {},
    ) =>
      this.request<V2TestPoolConnectionResponse, RpcStatus>({
        path: `/api/v2/pools/${id}/test`,
        method: "POST",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Pools
     * @name PoolServiceUpdatePool
     * @summary UpdatePool updates an existing pool configuration.
     * @request PUT:/api/v2/pools/{pool.id}
     */
    poolServiceUpdatePool: (
      { poolId }: PoolServiceUpdatePoolParams,
      body: PoolServiceUpdatePoolBody,
      params: RequestParams = {},
    ) =>
      this.request<V2UpdatePoolResponse, RpcStatus>({
        path: `/api/v2/pools/${poolId}`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceMuteContainerAlarm
     * @summary MuteContainerAlarm pulses the container controller's alarm mute. CT1 only.
     * @request POST:/api/v2/power/container/alarm/mute
     */
    powerServiceMuteContainerAlarm: (
      body: V2MuteContainerAlarmRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2MuteContainerAlarmResponse, RpcStatus>({
        path: `/api/v2/power/container/alarm/mute`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceResetContainerAlarm
     * @summary ResetContainerAlarm pulses the container controller's alarm reset. CT1 only.
     * @request POST:/api/v2/power/container/alarm/reset
     */
    powerServiceResetContainerAlarm: (
      body: V2ResetContainerAlarmRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2ResetContainerAlarmResponse, RpcStatus>({
        path: `/api/v2/power/container/alarm/reset`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceGetContainerControls
     * @summary GetContainerControls returns the tank/container plant controls (boolean switches) and analog setpoints exposed by the container backend, with their current states where the backend can read them back. Backends without control support report supported=false with empty lists. CT1 only.
     * @request GET:/api/v2/power/container/controls
     */
    powerServiceGetContainerControls: (params: RequestParams = {}) =>
      this.request<V2GetContainerControlsResponse, RpcStatus>({
        path: `/api/v2/power/container/controls`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceSetContainerControl
     * @summary SetContainerControl switches one boolean tank/container control (tank_led_light, coolant_pump, dry_cooler, dry_cooler_auto, air_conditioner_1, or air_conditioner_2) and echoes the state written. The PLC is authoritative for plant protection and command acceptance. CT1 only.
     * @request POST:/api/v2/power/container/controls/{name}
     */
    powerServiceSetContainerControl: (
      { name }: PowerServiceSetContainerControlParams,
      body: PowerServiceSetContainerControlBody,
      params: RequestParams = {},
    ) =>
      this.request<V2SetContainerControlResponse, RpcStatus>({
        path: `/api/v2/power/container/controls/${name}`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceRunDryCoolerFanReverse
     * @summary RunDryCoolerFanReverse applies a retained 3-10 whole-minute duration, starts the PLC-owned procedure, and returns after terminal completion. The operation never exposes or writes a manual stop command. CT1 only.
     * @request POST:/api/v2/power/container/dry-cooler/fan-reverse
     */
    powerServiceRunDryCoolerFanReverse: (
      body: V2RunDryCoolerFanReverseRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2RunDryCoolerFanReverseResponse, RpcStatus>({
        path: `/api/v2/power/container/dry-cooler/fan-reverse`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceSetMainCircuitBreakerState
     * @summary SetMainCircuitBreakerState requests an idempotent closed/open target and returns the controller's authoritative closed-state readback. CT1 only.
     * @request POST:/api/v2/power/container/main-circuit-breaker
     */
    powerServiceSetMainCircuitBreakerState: (
      body: V2SetMainCircuitBreakerStateRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetMainCircuitBreakerStateResponse, RpcStatus>({
        path: `/api/v2/power/container/main-circuit-breaker`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceSetContainerSetpoint
     * @summary SetContainerSetpoint writes one analog tank/container setpoint and echoes the applied value. Temperature setpoints are Celsius. Frigate validates only the finite float32 wire representation; the PLC owns operational limits and final command acceptance. CT1 only.
     * @request POST:/api/v2/power/container/setpoints/{name}
     */
    powerServiceSetContainerSetpoint: (
      { name }: PowerServiceSetContainerSetpointParams,
      body: PowerServiceSetContainerSetpointBody,
      params: RequestParams = {},
    ) =>
      this.request<V2SetContainerSetpointResponse, RpcStatus>({
        path: `/api/v2/power/container/setpoints/${name}`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power
     * @name PowerServiceGetDevicePower
     * @summary GetDevicePower returns aggregate device power consumption.
     * @request GET:/api/v2/power/device
     */
    powerServiceGetDevicePower: (params: RequestParams = {}) =>
      this.request<V2GetDevicePowerResponse, RpcStatus>({
        path: `/api/v2/power/device`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceListPowerFeeds
     * @summary ListPowerFeeds returns power-feed state and optional electrical telemetry.
     * @request GET:/api/v2/power/feeds
     */
    powerServiceListPowerFeeds: (params: RequestParams = {}) =>
      this.request<V2ListPowerFeedsResponse, RpcStatus>({
        path: `/api/v2/power/feeds`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceClearPowerFeedOverride
     * @summary ClearPowerFeedOverride removes the process-local operator marker from one power feed without changing its backend target or physical state. CT1 only.
     * @request POST:/api/v2/power/feeds/{powerFeedId}/override/clear
     */
    powerServiceClearPowerFeedOverride: (
      { powerFeedId }: PowerServiceClearPowerFeedOverrideParams,
      body: PowerServiceClearPowerFeedOverrideBody,
      params: RequestParams = {},
    ) =>
      this.request<V2ClearPowerFeedOverrideResponse, RpcStatus>({
        path: `/api/v2/power/feeds/${powerFeedId}/override/clear`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceSetPowerFeedState
     * @summary SetPowerFeedState enables or disables one container power feed.
     * @request POST:/api/v2/power/feeds/{powerFeedId}/state
     */
    powerServiceSetPowerFeedState: (
      { powerFeedId }: PowerServiceSetPowerFeedStateParams,
      body: PowerServiceSetPowerFeedStateBody,
      params: RequestParams = {},
    ) =>
      this.request<V2SetPowerFeedStateResponse, RpcStatus>({
        path: `/api/v2/power/feeds/${powerFeedId}/state`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceCyclePowerFeed
     * @summary CyclePowerFeed disables one enabled container power feed, holds it off for 30 seconds, and restores it as one durable Frigate-owned operation. The restore survives disconnects, restart, and HA handoff. CT1 only.
     * @request POST:/api/v2/power/feeds/{powerFeedId}:cycle
     */
    powerServiceCyclePowerFeed: (
      { powerFeedId, cycle }: PowerServiceCyclePowerFeedParams,
      body: PowerServiceCyclePowerFeedBody,
      params: RequestParams = {},
    ) =>
      this.request<V2CyclePowerFeedResponse, RpcStatus>({
        path: `/api/v2/power/feeds/${powerFeedId}${cycle}`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power
     * @name PowerServiceListHashboardPower
     * @summary ListHashboardPower returns per-hashboard power consumption.
     * @request GET:/api/v2/power/hashboards
     */
    powerServiceListHashboardPower: (params: RequestParams = {}) =>
      this.request<V2ListHashboardPowerResponse, RpcStatus>({
        path: `/api/v2/power/hashboards`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Power, CT1
     * @name PowerServiceGetActivePowerRecoveryCycle
     * @summary GetActivePowerRecoveryCycle returns the singleton durable outlet restore obligation, whether initiated by an operator or automatic recovery.
     * @request GET:/api/v2/power/recovery/active
     */
    powerServiceGetActivePowerRecoveryCycle: (params: RequestParams = {}) =>
      this.request<V2GetActivePowerRecoveryCycleResponse, RpcStatus>({
        path: `/api/v2/power/recovery/active`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags PSU Firmware, CT1
     * @name PsuFirmwareServiceGetPsuFirmwareBoardStatus
     * @summary GetPSUFirmwareBoardStatus reads one locally owned board's PSU firmware staging, apply, and leadership state.
     * @request GET:/api/v2/psu-firmware/boards/{macAddress}/status
     */
    psuFirmwareServiceGetPsuFirmwareBoardStatus: (
      { macAddress }: PsuFirmwareServiceGetPsuFirmwareBoardStatusParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetPSUFirmwareBoardStatusResponse, RpcStatus>({
        path: `/api/v2/psu-firmware/boards/${macAddress}/status`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags PSU Firmware, CT1
     * @name PsuFirmwareServiceListPsuFirmwareCapsules
     * @summary ListPSUFirmwareCapsules lists capsules cached on the connected node.
     * @request GET:/api/v2/psu-firmware/capsules
     */
    psuFirmwareServiceListPsuFirmwareCapsules: (params: RequestParams = {}) =>
      this.request<V2ListPSUFirmwareCapsulesResponse, RpcStatus>({
        path: `/api/v2/psu-firmware/capsules`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags PSU Firmware, CT1
     * @name PsuFirmwareServiceListPsuFirmwareUpdates
     * @summary ListPSUFirmwareUpdates lists active and retained update jobs, newest first.
     * @request GET:/api/v2/psu-firmware/updates
     */
    psuFirmwareServiceListPsuFirmwareUpdates: (params: RequestParams = {}) =>
      this.request<V2ListPSUFirmwareUpdatesResponse, RpcStatus>({
        path: `/api/v2/psu-firmware/updates`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags PSU Firmware, CT1
     * @name PsuFirmwareServiceStartPsuFirmwareUpdate
     * @summary StartPSUFirmwareUpdate starts one module update with a cached capsule. Every connected module member must be owned by the connected node.
     * @request POST:/api/v2/psu-firmware/updates
     */
    psuFirmwareServiceStartPsuFirmwareUpdate: (
      body: V2StartPSUFirmwareUpdateRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2StartPSUFirmwareUpdateResponse, RpcStatus>({
        path: `/api/v2/psu-firmware/updates`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags PSU Firmware, CT1
     * @name PsuFirmwareServiceGetPsuFirmwareUpdate
     * @summary GetPSUFirmwareUpdate returns the current state of one update job.
     * @request GET:/api/v2/psu-firmware/updates/{taskId}
     */
    psuFirmwareServiceGetPsuFirmwareUpdate: (
      { taskId }: PsuFirmwareServiceGetPsuFirmwareUpdateParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetPSUFirmwareUpdateResponse, RpcStatus>({
        path: `/api/v2/psu-firmware/updates/${taskId}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags PSU Firmware, CT1
     * @name PsuFirmwareServiceCancelPsuFirmwareUpdate
     * @summary CancelPSUFirmwareUpdate cancels a job before its apply command is sent. Once the leader has been asked to program the PSU, board firmware owns completion and the job can no longer be cancelled.
     * @request POST:/api/v2/psu-firmware/updates/{taskId}/cancel
     */
    psuFirmwareServiceCancelPsuFirmwareUpdate: (
      { taskId }: PsuFirmwareServiceCancelPsuFirmwareUpdateParams,
      params: RequestParams = {},
    ) =>
      this.request<V2CancelPSUFirmwareUpdateResponse, RpcStatus>({
        path: `/api/v2/psu-firmware/updates/${taskId}/cancel`,
        method: "POST",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags PSU Firmware, CT1
     * @name PsuFirmwareServiceBeginPsuFirmwareCapsuleUpload
     * @summary BeginPSUFirmwareCapsuleUpload starts a chunked unary capsule upload for clients that cannot send client-streaming calls, such as browsers using the Connect or REST gateways. It validates the same metadata as UploadPSUFirmwareCapsule and returns an upload ID that exists only on the connected node. Idle uploads expire.
     * @request POST:/api/v2/psu-firmware/uploads
     */
    psuFirmwareServiceBeginPsuFirmwareCapsuleUpload: (
      body: V2BeginPSUFirmwareCapsuleUploadRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2BeginPSUFirmwareCapsuleUploadResponse, RpcStatus>({
        path: `/api/v2/psu-firmware/uploads`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags PSU Firmware, CT1
     * @name PsuFirmwareServiceAbortPsuFirmwareCapsuleUpload
     * @summary AbortPSUFirmwareCapsuleUpload discards an unfinished upload. Aborting an unknown or expired upload succeeds.
     * @request DELETE:/api/v2/psu-firmware/uploads/{uploadId}
     */
    psuFirmwareServiceAbortPsuFirmwareCapsuleUpload: (
      { uploadId }: PsuFirmwareServiceAbortPsuFirmwareCapsuleUploadParams,
      params: RequestParams = {},
    ) =>
      this.request<V2AbortPSUFirmwareCapsuleUploadResponse, RpcStatus>({
        path: `/api/v2/psu-firmware/uploads/${uploadId}`,
        method: "DELETE",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags PSU Firmware, CT1
     * @name PsuFirmwareServiceWritePsuFirmwareCapsuleUploadChunk
     * @summary WritePSUFirmwareCapsuleUploadChunk writes capsule bytes at an exact offset. Repeating an already received chunk is accepted, so a client can safely retry after a lost response.
     * @request POST:/api/v2/psu-firmware/uploads/{uploadId}/chunks
     */
    psuFirmwareServiceWritePsuFirmwareCapsuleUploadChunk: (
      { uploadId }: PsuFirmwareServiceWritePsuFirmwareCapsuleUploadChunkParams,
      body: PSUFirmwareServiceWritePSUFirmwareCapsuleUploadChunkBody,
      params: RequestParams = {},
    ) =>
      this.request<V2WritePSUFirmwareCapsuleUploadChunkResponse, RpcStatus>({
        path: `/api/v2/psu-firmware/uploads/${uploadId}/chunks`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags PSU Firmware, CT1
     * @name PsuFirmwareServiceCommitPsuFirmwareCapsuleUpload
     * @summary CommitPSUFirmwareCapsuleUpload verifies and caches a complete capsule, exactly like the final step of UploadPSUFirmwareCapsule. An incomplete upload is refused without discarding received bytes.
     * @request POST:/api/v2/psu-firmware/uploads/{uploadId}/commit
     */
    psuFirmwareServiceCommitPsuFirmwareCapsuleUpload: (
      { uploadId }: PsuFirmwareServiceCommitPsuFirmwareCapsuleUploadParams,
      params: RequestParams = {},
    ) =>
      this.request<V2CommitPSUFirmwareCapsuleUploadResponse, RpcStatus>({
        path: `/api/v2/psu-firmware/uploads/${uploadId}/commit`,
        method: "POST",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Hashboards, CT1
     * @name HashboardServiceListRecoveryEvidence
     * @summary ListRecoveryEvidence returns bounded summaries of evidence snapshots taken before automatic hashboard recovery acted, newest first.
     * @request GET:/api/v2/recovery-evidence
     */
    hashboardServiceListRecoveryEvidence: (
      query: HashboardServiceListRecoveryEvidenceParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2ListRecoveryEvidenceResponse, RpcStatus>({
        path: `/api/v2/recovery-evidence`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Hashboards, CT1
     * @name HashboardServiceGetRecoveryEvidence
     * @summary GetRecoveryEvidence returns one recovery evidence snapshot, including its bounded JSON payload.
     * @request GET:/api/v2/recovery-evidence/{evidenceId}
     */
    hashboardServiceGetRecoveryEvidence: (
      { evidenceId }: HashboardServiceGetRecoveryEvidenceParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetRecoveryEvidenceResponse, RpcStatus>({
        path: `/api/v2/recovery-evidence/${evidenceId}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Run-In, CT1
     * @name RunInServiceListHashboardRunInTasks
     * @summary ListHashboardRunInTasks lists active and completed run-in tasks.
     * @request GET:/api/v2/run-in
     */
    runInServiceListHashboardRunInTasks: (
      query: RunInServiceListHashboardRunInTasksParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2ListHashboardRunInTasksResponse, RpcStatus>({
        path: `/api/v2/run-in`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Run-In, CT1
     * @name RunInServiceGetHashboardRunInStatus
     * @summary GetHashboardRunInStatus returns the current status for a run-in task.
     * @request GET:/api/v2/run-in/{taskId}
     */
    runInServiceGetHashboardRunInStatus: (
      { taskId, ...query }: RunInServiceGetHashboardRunInStatusParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetHashboardRunInStatusResponse, RpcStatus>({
        path: `/api/v2/run-in/${taskId}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Run-In, CT1
     * @name RunInServiceCancelHashboardRunIn
     * @summary CancelHashboardRunIn requests cancellation for a run-in task.
     * @request POST:/api/v2/run-in/{taskId}/cancel
     */
    runInServiceCancelHashboardRunIn: (
      { taskId, ...query }: RunInServiceCancelHashboardRunInParams,
      params: RequestParams = {},
    ) =>
      this.request<V2CancelHashboardRunInResponse, RpcStatus>({
        path: `/api/v2/run-in/${taskId}/cancel`,
        method: "POST",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description Downloads the completed run-in result as a gzip-compressed SQLite database. Newly completed artifacts are published atomically after finalization and compression succeed; stored legacy SQLite artifacts remain readable.
     *
     * @tags Run-In, CT1
     * @name RunInServiceDownloadHashboardRunInResults
     * @summary DownloadHashboardRunInResults downloads the final compressed SQLite result artifact for a run-in task.
     * @request GET:/api/v2/run-in/{taskId}/results.sqlite
     */
    runInServiceDownloadHashboardRunInResults: (
      { taskId }: RunInServiceDownloadHashboardRunInResultsParams,
      params: RequestParams = {},
    ) =>
      this.request<Blob, void | RpcStatus>({
        path: `/api/v2/run-in/${taskId}/results.sqlite`,
        method: "GET",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System
     * @name SystemServiceListAuditEvents
     * @summary ListAuditEvents returns recent audit-log entries, newest first.
     * @request GET:/api/v2/system/audit
     */
    systemServiceListAuditEvents: (
      query: SystemServiceListAuditEventsParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2ListAuditEventsResponse, RpcStatus>({
        path: `/api/v2/system/audit`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceGetCoolingConfig
     * @summary GetCoolingConfig returns the cooling configuration.
     * @request GET:/api/v2/system/cooling
     */
    systemServiceGetCoolingConfig: (params: RequestParams = {}) =>
      this.request<V2GetCoolingConfigResponse, RpcStatus>({
        path: `/api/v2/system/cooling`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceSetCoolingConfig
     * @summary SetCoolingConfig updates the cooling configuration.
     * @request PUT:/api/v2/system/cooling
     */
    systemServiceSetCoolingConfig: (
      body: V2SetCoolingConfigRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetCoolingConfigResponse, RpcStatus>({
        path: `/api/v2/system/cooling`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceApplyFirmwareUpdate
     * @summary ApplyFirmwareUpdate downloads and installs the selected firmware version.
     * @request POST:/api/v2/system/firmware/apply
     */
    systemServiceApplyFirmwareUpdate: (
      body: V2ApplyFirmwareUpdateRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2ApplyFirmwareUpdateResponse, RpcStatus>({
        path: `/api/v2/system/firmware/apply`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceCheckFirmwareUpdate
     * @summary CheckFirmwareUpdate checks for available firmware updates.
     * @request GET:/api/v2/system/firmware/check
     */
    systemServiceCheckFirmwareUpdate: (params: RequestParams = {}) =>
      this.request<V2CheckFirmwareUpdateResponse, RpcStatus>({
        path: `/api/v2/system/firmware/check`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceWatchFirmwareUpdate
     * @summary WatchFirmwareUpdate streams the current status and changes for one update. Reconnect with the same operation ID after interruption or ordinary reboot. A clear-settings reboot erases the operation history.
     * @request GET:/api/v2/system/firmware/updates/{operationId}/watch
     */
    systemServiceWatchFirmwareUpdate: (
      { operationId }: SystemServiceWatchFirmwareUpdateParams,
      params: RequestParams = {},
    ) =>
      this.request<
        {
          /**
           * The `Status` type defines a logical error model that is suitable for
           * different programming environments, including REST APIs and RPC APIs. It is
           * used by [gRPC](https://github.com/grpc). Each `Status` message contains
           * three pieces of data: error code, error message, and error details.
           *
           * You can find out more about this error model and how to work with it in the
           * [API Design Guide](https://cloud.google.com/apis/design/errors).
           */
          error?: RpcStatus;
          result?: V2WatchFirmwareUpdateResponse;
        },
        RpcStatus
      >({
        path: `/api/v2/system/firmware/updates/${operationId}/watch`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, CT1
     * @name SystemServiceGetHaStatus
     * @summary GetHAStatus returns this server's cached HA identity and observations.
     * @request GET:/api/v2/system/ha
     */
    systemServiceGetHaStatus: (params: RequestParams = {}) =>
      this.request<V2GetHAStatusResponse, RpcStatus>({
        path: `/api/v2/system/ha`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System
     * @name SystemServiceGetHardwareInfo
     * @summary GetHardwareInfo returns hardware details.
     * @request GET:/api/v2/system/hardware
     */
    systemServiceGetHardwareInfo: (params: RequestParams = {}) =>
      this.request<V2GetHardwareInfoResponse, RpcStatus>({
        path: `/api/v2/system/hardware`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System
     * @name SystemServiceGetSystemInfo
     * @summary GetSystemInfo returns static system information.
     * @request GET:/api/v2/system/info
     */
    systemServiceGetSystemInfo: (params: RequestParams = {}) =>
      this.request<V2GetSystemInfoResponse, RpcStatus>({
        path: `/api/v2/system/info`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceLocateDevice
     * @summary LocateDevice activates the device locator LED.
     * @request POST:/api/v2/system/locate
     */
    systemServiceLocateDevice: (
      body: V2LocateDeviceRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2LocateDeviceResponse, RpcStatus>({
        path: `/api/v2/system/locate`,
        method: "POST",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System
     * @name SystemServiceGetLogs
     * @summary GetLogs returns a stream of system log entries.
     * @request GET:/api/v2/system/logs
     */
    systemServiceGetLogs: (
      query: SystemServiceGetLogsParams = {},
      params: RequestParams = {},
    ) =>
      this.request<
        {
          /**
           * The `Status` type defines a logical error model that is suitable for
           * different programming environments, including REST APIs and RPC APIs. It is
           * used by [gRPC](https://github.com/grpc). Each `Status` message contains
           * three pieces of data: error code, error message, and error details.
           *
           * You can find out more about this error model and how to work with it in the
           * [API Design Guide](https://cloud.google.com/apis/design/errors).
           */
          error?: RpcStatus;
          /** GetLogsResponse contains a single log entry from the stream. */
          result?: V2GetLogsResponse;
        },
        RpcStatus
      >({
        path: `/api/v2/system/logs`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceGetNetworkConfig
     * @summary GetNetworkConfig returns the network configuration.
     * @request GET:/api/v2/system/network
     */
    systemServiceGetNetworkConfig: (params: RequestParams = {}) =>
      this.request<V2GetNetworkConfigResponse, RpcStatus>({
        path: `/api/v2/system/network`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceSetNetworkConfig
     * @summary SetNetworkConfig updates the network configuration.
     * @request PUT:/api/v2/system/network
     */
    systemServiceSetNetworkConfig: (
      body: V2SetNetworkConfigRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetNetworkConfigResponse, RpcStatus>({
        path: `/api/v2/system/network`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System
     * @name SystemServiceListPowerSupplies
     * @summary ListPowerSupplies returns information about installed PSUs.
     * @request GET:/api/v2/system/psu
     */
    systemServiceListPowerSupplies: (params: RequestParams = {}) =>
      this.request<V2ListPowerSuppliesResponse, RpcStatus>({
        path: `/api/v2/system/psu`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System
     * @name SystemServiceUpdatePsuFirmware
     * @summary UpdatePSUFirmware initiates a firmware update on a PSU.
     * @request POST:/api/v2/system/psu/{psuId}/update
     */
    systemServiceUpdatePsuFirmware: (
      { psuId }: SystemServiceUpdatePsuFirmwareParams,
      params: RequestParams = {},
    ) =>
      this.request<V2UpdatePSUFirmwareResponse, RpcStatus>({
        path: `/api/v2/system/psu/${psuId}/update`,
        method: "POST",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceReboot
     * @summary Reboot reboots the device.
     * @request POST:/api/v2/system/reboot
     */
    systemServiceReboot: (params: RequestParams = {}) =>
      this.request<V2RebootResponse, RpcStatus>({
        path: `/api/v2/system/reboot`,
        method: "POST",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceGetSshConfig
     * @summary GetSSHConfig returns the SSH configuration.
     * @request GET:/api/v2/system/ssh
     */
    systemServiceGetSshConfig: (params: RequestParams = {}) =>
      this.request<V2GetSSHConfigResponse, RpcStatus>({
        path: `/api/v2/system/ssh`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceSetSshConfig
     * @summary SetSSHConfig updates the SSH configuration.
     * @request PUT:/api/v2/system/ssh
     */
    systemServiceSetSshConfig: (
      body: V2SetSSHConfigRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetSSHConfigResponse, RpcStatus>({
        path: `/api/v2/system/ssh`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System
     * @name SystemServiceGetSystemStatus
     * @summary GetSystemStatus returns current system status.
     * @request GET:/api/v2/system/status
     */
    systemServiceGetSystemStatus: (params: RequestParams = {}) =>
      this.request<V2GetSystemStatusResponse, RpcStatus>({
        path: `/api/v2/system/status`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System
     * @name SystemServiceListTags
     * @summary ListTags returns the system's tag set.
     * @request GET:/api/v2/system/tags
     */
    systemServiceListTags: (params: RequestParams = {}) =>
      this.request<V2ListTagsResponse, RpcStatus>({
        path: `/api/v2/system/tags`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System
     * @name SystemServiceSetTags
     * @summary SetTags replaces the system's tag set.
     * @request PUT:/api/v2/system/tags
     */
    systemServiceSetTags: (
      body: V2SetTagsRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetTagsResponse, RpcStatus>({
        path: `/api/v2/system/tags`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags System, R2
     * @name SystemServiceGetUnlockStatus
     * @summary GetUnlockStatus returns the R2 hardware lock status. CT1 Frigate servers do not implement this R2-only RPC. Implementations without an authoritative hardware observation must report LOCK_STATE_UNKNOWN, never infer UNLOCKED.
     * @request GET:/api/v2/system/unlock
     */
    systemServiceGetUnlockStatus: (params: RequestParams = {}) =>
      this.request<V2GetUnlockStatusResponse, RpcStatus>({
        path: `/api/v2/system/unlock`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Hashboards, CT1
     * @name HashboardServiceListUnitTags
     * @summary ListUnitTags returns operator tags for modules, hashboards, and container power feeds.
     * @request GET:/api/v2/tags
     */
    hashboardServiceListUnitTags: (params: RequestParams = {}) =>
      this.request<V2ListUnitTagsResponse, RpcStatus>({
        path: `/api/v2/tags`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Hashboards, CT1
     * @name HashboardServiceSetUnitTag
     * @summary SetUnitTag upserts an operator tag for a module (keyed by module ID), a hashboard (keyed by MAC address), or a container power feed (keyed by backend feed ID). An empty tag deletes the entry.
     * @request PUT:/api/v2/tags
     */
    hashboardServiceSetUnitTag: (
      body: V2SetUnitTagRequest,
      params: RequestParams = {},
    ) =>
      this.request<V2SetUnitTagResponse, RpcStatus>({
        path: `/api/v2/tags`,
        method: "PUT",
        body: body,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry, CT1
     * @name TelemetryServiceGetContainerHealth
     * @summary GetContainerHealth returns a point-in-time plant and power-feed snapshot.
     * @request GET:/api/v2/telemetry/container
     */
    telemetryServiceGetContainerHealth: (
      query: TelemetryServiceGetContainerHealthParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2GetContainerHealthResponse, RpcStatus>({
        path: `/api/v2/telemetry/container`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry, CT1
     * @name TelemetryServiceListContainerPlantAlarmEpisodes
     * @summary ListContainerPlantAlarmEpisodes reconstructs active and cleared PLC alarm intervals from the durable transition journal.
     * @request GET:/api/v2/telemetry/container/alarms
     */
    telemetryServiceListContainerPlantAlarmEpisodes: (
      query: TelemetryServiceListContainerPlantAlarmEpisodesParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2ListContainerPlantAlarmEpisodesResponse, RpcStatus>({
        path: `/api/v2/telemetry/container/alarms`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry, CT1
     * @name TelemetryServiceListContainerPlantEvents
     * @summary ListContainerPlantEvents returns durable control, setpoint, alarm, fault, power-feed, and observed-state transitions.
     * @request GET:/api/v2/telemetry/container/events
     */
    telemetryServiceListContainerPlantEvents: (
      query: TelemetryServiceListContainerPlantEventsParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2ListContainerPlantEventsResponse, RpcStatus>({
        path: `/api/v2/telemetry/container/events`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry, CT1
     * @name TelemetryServiceStreamContainerHealth
     * @summary StreamContainerHealth streams container plant and power-feed telemetry.
     * @request GET:/api/v2/telemetry/container/stream
     */
    telemetryServiceStreamContainerHealth: (params: RequestParams = {}) =>
      this.request<
        {
          /**
           * The `Status` type defines a logical error model that is suitable for
           * different programming environments, including REST APIs and RPC APIs. It is
           * used by [gRPC](https://github.com/grpc). Each `Status` message contains
           * three pieces of data: error code, error message, and error details.
           *
           * You can find out more about this error model and how to work with it in the
           * [API Design Guide](https://cloud.google.com/apis/design/errors).
           */
          error?: RpcStatus;
          /** StreamContainerHealthResponse contains plant and power-feed telemetry. */
          result?: V2StreamContainerHealthResponse;
        },
        RpcStatus
      >({
        path: `/api/v2/telemetry/container/stream`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry
     * @name TelemetryServiceStreamDeviceMetrics
     * @summary StreamDeviceMetrics streams aggregate device-level metrics.
     * @request GET:/api/v2/telemetry/device/stream
     */
    telemetryServiceStreamDeviceMetrics: (params: RequestParams = {}) =>
      this.request<
        {
          /**
           * The `Status` type defines a logical error model that is suitable for
           * different programming environments, including REST APIs and RPC APIs. It is
           * used by [gRPC](https://github.com/grpc). Each `Status` message contains
           * three pieces of data: error code, error message, and error details.
           *
           * You can find out more about this error model and how to work with it in the
           * [API Design Guide](https://cloud.google.com/apis/design/errors).
           */
          error?: RpcStatus;
          /** StreamDeviceMetricsResponse contains aggregate device-level metrics. */
          result?: V2StreamDeviceMetricsResponse;
        },
        RpcStatus
      >({
        path: `/api/v2/telemetry/device/stream`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry
     * @name TelemetryServiceQueryTankSensorHistory
     * @summary QueryTankSensorHistory reads source-identified C1 acquisitions from the owning Frigate's durable PostgreSQL/TimescaleDB store.
     * @request GET:/api/v2/telemetry/foghashing/tanks/{tankKey}/history
     */
    telemetryServiceQueryTankSensorHistory: (
      { tankKey, ...query }: TelemetryServiceQueryTankSensorHistoryParams,
      params: RequestParams = {},
    ) =>
      this.request<V2QueryTankSensorHistoryResponse, RpcStatus>({
        path: `/api/v2/telemetry/foghashing/tanks/${tankKey}/history`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry
     * @name TelemetryServiceQueryTankProductionHistory
     * @request GET:/api/v2/telemetry/foghashing/tanks/{tankKey}/production
     */
    telemetryServiceQueryTankProductionHistory: (
      { tankKey, ...query }: TelemetryServiceQueryTankProductionHistoryParams,
      params: RequestParams = {},
    ) =>
      this.request<V2QueryTankProductionHistoryResponse, RpcStatus>({
        path: `/api/v2/telemetry/foghashing/tanks/${tankKey}/production`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry
     * @name TelemetryServiceStreamHashboardHealth
     * @summary StreamHashboardHealth streams hashboard health snapshots at ~1Hz.
     * @request GET:/api/v2/telemetry/hashboards/stream
     */
    telemetryServiceStreamHashboardHealth: (
      query: TelemetryServiceStreamHashboardHealthParams = {},
      params: RequestParams = {},
    ) =>
      this.request<
        {
          /**
           * The `Status` type defines a logical error model that is suitable for
           * different programming environments, including REST APIs and RPC APIs. It is
           * used by [gRPC](https://github.com/grpc). Each `Status` message contains
           * three pieces of data: error code, error message, and error details.
           *
           * You can find out more about this error model and how to work with it in the
           * [API Design Guide](https://cloud.google.com/apis/design/errors).
           */
          error?: RpcStatus;
          /** StreamHashboardHealthResponse contains a health snapshot for a single hashboard. */
          result?: V2StreamHashboardHealthResponse;
        },
        RpcStatus
      >({
        path: `/api/v2/telemetry/hashboards/stream`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry
     * @name TelemetryServiceStreamAsicHealth
     * @summary StreamASICHealth streams per-ASIC health data for a single hashboard.
     * @request GET:/api/v2/telemetry/hashboards/{identifier}/asics/stream
     */
    telemetryServiceStreamAsicHealth: (
      { identifier }: TelemetryServiceStreamAsicHealthParams,
      params: RequestParams = {},
    ) =>
      this.request<
        {
          /**
           * The `Status` type defines a logical error model that is suitable for
           * different programming environments, including REST APIs and RPC APIs. It is
           * used by [gRPC](https://github.com/grpc). Each `Status` message contains
           * three pieces of data: error code, error message, and error details.
           *
           * You can find out more about this error model and how to work with it in the
           * [API Design Guide](https://cloud.google.com/apis/design/errors).
           */
          error?: RpcStatus;
          /** StreamASICHealthResponse contains per-ASIC health data for a hashboard. */
          result?: V2StreamASICHealthResponse;
        },
        RpcStatus
      >({
        path: `/api/v2/telemetry/hashboards/${identifier}/asics/stream`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry
     * @name TelemetryServiceGetHashboardLifetime
     * @summary GetHashboardLifetime returns the cumulative runtime the service has observed for one hashboard. It is preserved independently of retained raw health samples and excludes unobserved telemetry gaps.
     * @request GET:/api/v2/telemetry/hashboards/{identifier}/lifetime
     */
    telemetryServiceGetHashboardLifetime: (
      { identifier }: TelemetryServiceGetHashboardLifetimeParams,
      params: RequestParams = {},
    ) =>
      this.request<V2GetHashboardLifetimeResponse, RpcStatus>({
        path: `/api/v2/telemetry/hashboards/${identifier}/lifetime`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry, CT1
     * @name TelemetryServiceQueryProductionHistory
     * @summary QueryProductionHistory returns indefinitely retained hourly or daily production summaries. An optional identifier selects one CT1 board; otherwise the result is the container aggregate.
     * @request GET:/api/v2/telemetry/production/history
     */
    telemetryServiceQueryProductionHistory: (
      query: TelemetryServiceQueryProductionHistoryParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2QueryProductionHistoryResponse, RpcStatus>({
        path: `/api/v2/telemetry/production/history`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry, CT1
     * @name TelemetryServiceQueryShareQualityHistory
     * @summary QueryShareQualityHistory returns indefinitely retained share-quality summaries without scanning raw nonces and work IDs.
     * @request GET:/api/v2/telemetry/shares/history
     */
    telemetryServiceQueryShareQualityHistory: (
      query: TelemetryServiceQueryShareQualityHistoryParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2QueryShareQualityHistoryResponse, RpcStatus>({
        path: `/api/v2/telemetry/shares/history`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry
     * @name TelemetryServiceGetShareQuality
     * @summary GetShareQuality returns share acceptance, rejection, and accepted-hashrate statistics over a window.
     * @request GET:/api/v2/telemetry/shares/quality
     */
    telemetryServiceGetShareQuality: (
      query: TelemetryServiceGetShareQualityParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2GetShareQualityResponse, RpcStatus>({
        path: `/api/v2/telemetry/shares/quality`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Telemetry
     * @name TelemetryServiceQueryTimeSeries
     * @summary QueryTimeSeries queries historical time series data, returning bucketed aggregates (average, minimum, maximum, and sample count) over the requested metric, time range, and bucket size.
     * @request GET:/api/v2/telemetry/timeseries
     */
    telemetryServiceQueryTimeSeries: (
      query: TelemetryServiceQueryTimeSeriesParams = {},
      params: RequestParams = {},
    ) =>
      this.request<V2QueryTimeSeriesResponse, RpcStatus>({
        path: `/api/v2/telemetry/timeseries`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),
  };
}
