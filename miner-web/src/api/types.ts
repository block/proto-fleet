/* eslint-disable */
/* tslint:disable */
/*
 * ---------------------------------------------------------------
 * ## THIS FILE WAS GENERATED VIA SWAGGER-TYPESCRIPT-API        ##
 * ##                                                           ##
 * ## AUTHOR: acacode                                           ##
 * ## SOURCE: https://github.com/acacode/swagger-typescript-api ##
 * ---------------------------------------------------------------
 */

export interface Aggregates {
  /** Average value in data. */
  avg?: number;
  /** Maximum value in data. */
  max?: number;
  /** Minimum value in data. */
  min?: number;
}

export interface AsicStats {
  /**
   * Physical column location of the ASIC on the hashboard.
   * @example 10
   */
  column?: number;
  /**
   * The number of times that the ASIC produced an incorrect hash or an error during a specific period of time.  Error Rate (%) = (Number of incorrect hash / Total number of expected Hash) x 100%
   * @example 3.3
   */
  error_rate?: number;
  /**
   * The frequency of the ASIC measured in megahertz.
   * @example 650
   */
  freq_mhz?: number;
  /**
   * The current hash rate of the ASIC, measured in GH/s.
   * @example 300
   */
  hashrate_ghs?: number;
  /**
   * Unique identifier assigned to each ASIC located on a hashboard, starting from 0.
   * @example 0
   */
  id?: number;
  /**
   * The expected hashrate determined by the clock frequency of the ASIC, measured in GH/s.
   * @example 300
   */
  ideal_hashrate_ghs?: number;
  /**
   * Physical row location of the ASIC on the hashboard.
   * @example 0
   */
  row?: number;
  /**
   * Current temperature of the ASIC in celsius
   * @example 45.5
   */
  temp_c?: number;
}

export interface AsicStatsResponse {
  "asic-stats"?: AsicStats;
}

export interface AuthTokens {
  /** JWT access token. */
  access_token: string;
  /** JWT refresh token. */
  refresh_token: string;
}

export interface CoolingConfig {
  /**
   * Parameter to define the cooling mode.  Modes:
   *  - Off: Fans will be set to off for immersion cooling.
   *  - Auto: Fans will be controlled based on miner temperature.
   *  - Max: Fans will be run at full speed regardless of temperature.
   * @example "Auto"
   */
  mode?: "Off" | "Auto" | "Max";
}

export interface CoolingStatus {
  "cooling-status"?: CoolingStatusCoolingstatus;
}

export interface CoolingStatusCoolingstatus {
  /**
   * Parameter to show the current fan mode.
   * @example "Auto"
   */
  fan_mode?: "Off" | "Auto" | "Max";
  /** This will show speed of all fans in the system. */
  fans?: FanInfo[];
}

export interface EfficiencyResponse {
  "efficiency-data"?: EfficiencyResponseEfficiencydata;
}

export interface EfficiencyResponseEfficiencydata {
  aggregates?: Aggregates;
  data?: TimeSeriesData[];
  /** Duration of power data returned. */
  duration?: "12h" | "24h" | "48h" | "5d";
}

export interface Error {
  /**
   * Error code.
   * @example "INCORRECT_ARGS"
   */
  code?: string;
  /**
   * Error message.
   * @example "Arguments are incorrect for query."
   */
  message?: string;
}

export type ErrorListResponse = NotificationError[];

export interface ErrorResponse {
  error?: Error;
}

export interface FWInfo {
  /** @example "release" */
  build?: "debug" | "release";
  /** @example "1213423223" */
  git_hash?: string;
  /** @example "1213423223" */
  image_hash?: string;
  /** @example "1.0" */
  version?: string;
}

export interface FanInfo {
  /**
   * Each fan is assigned a unique identifier starting from 0.
   * @example 0
   */
  id?: number;
  /**
   * The fan's current rotations per minute (RPM).
   * @example 1200
   */
  rpm?: number;
}

export interface HashboardStats {
  "hashboard-stats"?: HashboardStatsHashboardstats;
}

export interface HashboardStatsHashboardstats {
  asics?: AsicStats[];
  /**
   * Current average temperature of the hashboard in celsius.
   * @example 75
   */
  avg_asic_temp_c?: number;
  /**
   * The efficiency of the hashboard in joules per terahash.
   * @example 40
   */
  efficiency_jth?: number;
  /**
   * The current hash rate of the hashboard, measured in GH/s. It will be sum of all ASIC hashrate_ghs values.
   * @example 300
   */
  hashrate_ghs?: number;
  /**
   * Internal ID of the hashboard, assigned to each hashboard starting from 0.
   * @example "YWWLMMMMRRFSSSSS"
   */
  hb_id?: string;
  /** Manufacturing serial number of the hashboard, used for subsequent API calls. */
  hb_sn?: string;
  /**
   * The expected hashrate is determined by the clock frequency of the all ASIC on the hash board, measured in GH/s.
   * @example 300
   */
  ideal_hashrate_ghs?: number;
  /**
   * The power consumption of the hashboard in watts.
   * @example 1000
   */
  power_usage_watts?: number;
  /**
   * The current state or condition of the hashboard.
   * @example "Running"
   */
  status?: "Running" | "Stopped" | "Error" | "Overheated" | "Unknown";
  /**
   * The present voltage being supplied to the hashboard in millivolts.
   * @example 16200
   */
  voltage_mv?: number;
}

export interface HashboardsInfo {
  "hashboards-info"?: HashboardsInfoHashboardsinfo[];
}

export interface HashboardsInfoHashboardsinfo {
  /** @example "1.0" */
  api_version?: string;
  /** @example "PROTO0_B" */
  board?: "NOT_SET" | "PROTO0_A" | "PROTO0_B" | "EVT" | "DVT" | "PVT" | "EVB" | "EPIC" | "EE_TEST";
  bootloader?: FWInfo;
  /** @example "ABC123" */
  chip_id?: string;
  /**
   * The absolute path where EC logs are stored.
   * @example "/var/log/ec_logs"
   */
  ec_logs_path?: string;
  firmware?: FWInfo;
  /**
   * Hashboard serial number.
   * @example "YWWLMMMMRRFSSSSS"
   */
  hb_sn?: string;
  /** @example "BZM" */
  mining_asic?: "BZM" | "MC1" | "MC2";
  /**
   * Number of asics on the hashboard.
   * @example 100
   */
  mining_asic_count?: number;
  /**
   * The USB port number the hashboard is connected to.
   * @example 0
   */
  port?: number;
  /**
   * Number of temperature sensors on the hashboard.
   * @example 3
   */
  temp_sensor_count?: number;
}

export interface HashrateResponse {
  "hashrate-data"?: HashrateResponseHashratedata;
}

export interface HashrateResponseHashratedata {
  aggregates?: Aggregates;
  data?: TimeSeriesData[];
  /** Duration of hashrate data returned. */
  duration?: "12h" | "24h" | "48h" | "5d";
}

export interface LogsResponse {
  logs?: LogsResponseLogs;
}

export interface LogsResponseLogs {
  content?: string[];
  /**
   * Number of lines returned.
   * @example 100
   */
  lines?: number;
  /**
   * Source of logs.
   * @example "miner_sw"
   */
  source?: string;
}

export interface MessageResponse {
  message?: string;
}

/** Mining statistics */
export interface MiningStatus {
  "mining-status"?: MiningStatusMiningstatus;
}

export interface MiningStatusMiningstatus {
  /**
   * Average temperature of the ASICs in the mining device.
   * @example 60
   */
  average_asic_temp_c?: number;
  /** The average efficiency in joules per terahash, since the device started mining. */
  average_efficiency_jth?: number;
  /**
   * The average hash rate in giga-hashes per second, since the device started mining. average_hashrate_ghs = Total hash count / (elapsed_time_s * 10^9)
   * @example 110000.2
   */
  average_hashrate_ghs?: number;
  /**
   * Average temperature of the mining device.
   * @example 60
   */
  average_hb_temp_c?: number;
  /**
   * The number of hardware errors that have occurred during the mining operation.
   * @example 100
   */
  hw_errors?: number;
  /**
   * Expected hashrate determined by the current power level.
   * @example 112000
   */
  ideal_hashrate_ghs?: number;
  /** @example "This reserved space can be utilized to include additional debug information." */
  message?: string;
  /**
   * The amount of time in seconds that has passed since the start of the mining operation.
   * @example 521
   */
  mining_uptime_s?: number;
  /**
   * Amount of power in watts for the system to target.
   * @example 3120
   */
  power_target_watts?: number;
  /**
   * Amount of power being consumed by mining in watts.
   * @example 3100
   */
  power_usage_watts?: number;
  /**
   * The amount of time in seconds that has passed since the last reboot of the system.
   * @example 521
   */
  reboot_uptime_s?: number;
  /**
   * The indication will reveal whether the mining operation is currently active or has ceased
   * @example "Mining"
   */
  status?:
    | "Uninitialized"
    | "PoweringOn"
    | "Mining"
    | "DegradedMining"
    | "PoweringOff"
    | "Stopped"
    | "NoPools"
    | "Error";
}

export interface MiningTarget {
  /** @example 3000 */
  power_target_watts?: number;
}

export interface NetworkConfig {
  "network-config"?: NetworkConfigNetworkconfig;
}

export interface NetworkConfigNetworkconfig {
  /** @example true */
  dhcp?: boolean;
  /** @example "172.27.244.177" */
  gateway?: string;
  /** @example "172.27.244.179" */
  ip?: string;
  /** @example "255.255.255.240" */
  netmask?: string;
}

export interface NetworkInfo {
  "network-info"?: NetworkInfoNetworkinfo;
}

export interface NetworkInfoNetworkinfo {
  /** @example true */
  dhcp?: boolean;
  /** @example "172.27.244.177" */
  gateway?: string;
  /** @example "172.27.244.179" */
  ip?: string;
  /** @example "82:11:D2:94:0D:6D" */
  mac?: string;
  /** @example "255.255.255.240" */
  netmask?: string;
}

export interface NotificationError {
  asic_index?: number;
  component_index?: number;
  /** @example "{"FanSlow":{"fan_rpm_target":1000,"fan_rpm_tach":900}}" */
  details?: string;
  /** @example "FanSlow" */
  error_code?: string;
  error_level?: "Error" | "Warning";
  expired_at?: number;
  hashboard_index?: number;
  inserted_at?: number;
  /** @example "Fan 1 is not operating correctly." */
  message?: string;
  /** @example "Miner" */
  source?: "Miner" | "Hashboard" | "ASIC";
}

export interface OSInfo {
  /** @example "20231208T220633Z" */
  build_datetime_utc?: string;
  /** @example "1213423223" */
  git_hash?: string;
  /** @example "btcm-c1-p0" */
  machine?: string;
  /** @example "BTCM Linux Distribution" */
  name?: string;
  status?: OSStatus;
  /** @example "release" */
  variant?: "release" | "mfg" | "dev" | "unknown";
  /** @example "1.0.1" */
  version?: string;
}

export interface OSStatus {
  /** @example 30.2 */
  cpu_load_percent?: number;
  /** @example 192784 */
  mem_free_kb?: number;
  /** @example 233712 */
  mem_total_kb?: number;
  /** @example 600 */
  rootfs_free_mb?: number;
  /** @example 1024 */
  rootfs_total_mb?: number;
}

export interface PasswordRequest {
  /**
   * The password for the user
   * @format password
   * @minLength 8
   */
  password: string;
}

export interface Pool {
  /**
   * The number of shares that have been accepted by the mining pool as valid solutions to a mining problem.
   * @example 100
   */
  accepted?: number;
  /**
   * The number of mined blocks seen during mining (not necessarily found by miner).
   * @example 10
   */
  blocks_seen?: number;
  /**
   * The current difficulty from the pool.
   * @example 134000
   */
  current_difficulty?: number;
  /**
   * The current number of works in use by the miner.
   * @example 134000
   */
  current_works?: number;
  /**
   * Each pool has a unique ID from 0 to 2, with 0 representing the highest priority and 2 representing the lowest priority.
   * @example 0
   */
  id?: number;
  /**
   * The number of shares the pool interface rejected due to being too low difficulty (did not forward to the pool).
   * @example 10
   */
  invalid?: number;
  /**
   * The number of notify messages (new jobs) received from the pool.
   * @example 10
   */
  notifys_received?: number;
  /** Connection priority for this pool. Lower numbers are higher priorities, with 0 being the maximum. Duplicate priorities are not allowed. */
  priority?: number;
  /** The protocol being used for communication with the mining pool. */
  protocol?: "Unknown" | "StratumV1" | "StratumV2";
  /**
   * The number of shares submitted by the miner to the pool that were not accepted because they did not meet the required difficulty level or other criteria.
   * @example 20
   */
  rejected?: number;
  /** The status field indicates the state of the mining pool. An "Idle" status indiciates that the pool is available but not currently in use (due to priority). An "Active" status means that the pool is currently active. A "Dead" status indicates that the mining device is unable to establish a connection with the pool. */
  status?: "Unknown" | "Idle" | "Active" | "Dead";
  /**
   * The pool URL is used to establish communication with the mining pool and it is essential that it includes the port information.
   * @example "pool1.com:3333"
   */
  url?: string;
  /**
   * The user is an account that is used for authentication with the mining pool. In some cases, if the user has multiple mining devices, the pool may assign a worker name as the username for each mining device.
   * @example "user1"
   */
  user?: string;
  /**
   * The number of works that were generated from the job notify messages.
   * @example 10
   */
  works_generated?: number;
}

export type PoolConfig = PoolConfigInner[];

export interface PoolConfigInner {
  /**
   * A password used for authentication and accessing the mining pool, which is ignored by SV1 pools.
   * @example "anything"
   */
  password?: string;
  /** Connection priority for this pool. Lower numbers are higher priorities, with 0 being the maximum. */
  priority?: number;
  /**
   * The pool URL is used to establish communication with the mining pool and it is essential that it includes the port information.
   * @example "pool1.com:3333"
   */
  url?: string;
  /**
   * The user is an account that is used for authentication with the mining pool. In some cases, if the user has multiple mining devices, the pool may assign a worker name as the username for each mining device.
   * @example "user1"
   */
  username?: string;
}

export type PoolConfigResponse = PoolConfigResponseInner[];

export interface PoolConfigResponseInner {
  /** Connection priority for this pool. Lower numbers are higher priorities, with 0 being the maximum. */
  priority?: number;
  /**
   * The pool URL is used to establish communication with the mining pool and it is essential that it includes the port information.
   * @example "pool1.com:3333"
   */
  url?: string;
  /**
   * The user is an account that is used for authentication with the mining pool. In some cases, if the user has multiple mining devices, the pool may assign a worker name as the username for each mining device.
   * @example "user1"
   */
  username?: string;
}

export interface PoolResponse {
  pool?: Pool;
}

export interface PoolsList {
  pools?: Pool[];
}

export interface PowerResponse {
  "power-data"?: PowerResponsePowerdata;
}

export interface PowerResponsePowerdata {
  aggregates?: Aggregates;
  data?: TimeSeriesData[];
  /** Duration of power data returned. */
  duration?: "12h" | "24h" | "48h" | "5d";
}

export interface RefreshRequest {
  /** The JWT refresh token to be validated. */
  refresh_token: string;
}

export interface RefreshResponse {
  /** A new JWT access token. */
  access_token: string;
}

export interface SWInfo {
  /** @example "Cgminer" */
  name?: string;
  /** @example "1.0" */
  version?: string;
}

export interface SshConfig {
  "ssh-status"?: SshStatus;
}

export interface SshResponse {
  "ssh-status"?: SshStatus;
}

export interface SshStatus {
  /** @example true */
  enabled?: boolean;
}

export interface SystemInfo {
  "system-info"?: SystemInfoSysteminfo;
}

export interface SystemInfoSysteminfo {
  /** @example "c1-evt" */
  board?: "stm32mp157d-dk1" | "stm32mp157f-dk2" | "c1-p0" | "c1-evt" | "unknown";
  /** @example "YWWLMMMMRRFSSSSS" */
  cb_sn?: string;
  mining_driver_sw?: SWInfo;
  os?: OSInfo;
  pool_interface_sw?: SWInfo;
  /** @example "STM32MP157F" */
  soc?: "STM32MP157F" | "STM32MP157D" | "STM32MP151F" | "STM32MP131F" | "unknown";
  /**
   * @format int64
   * @example 300
   */
  uptime_seconds?: number;
  web_server?: SWInfo;
}

export interface SystemStatuses {
  /** @example true */
  onboarded?: boolean;
}

export interface TemperatureResponse {
  "temperature-data"?: TemperatureResponseTemperaturedata;
}

export interface TemperatureResponseTemperaturedata {
  aggregates?: Aggregates;
  data?: TimeSeriesData[];
  /** Duration of temperature data returned. */
  duration?: "12h" | "24h" | "48h" | "5d";
}

export interface TestConnection {
  /**
   * A password used for authentication and accessing the mining pool, which is ignored by SV1 pools.
   * @example "anything"
   */
  password?: string;
  /**
   * The pool URL is used to establish communication with the mining pool and it is essential that it includes the port information.
   * @example "pool1.com:3333"
   */
  url?: string;
  /**
   * The user is an account that is used for authentication with the mining pool. In some cases, if the user has multiple mining devices, the pool may assign a worker name as the username for each mining device.
   * @example "user1"
   */
  username?: string;
}

export interface TimeSeriesData {
  /** Unix time epoch. */
  datetime?: number;
  /** Value of data requested at the given datetime. */
  value?: number;
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

export type RequestParams = Omit<FullRequestParams, "body" | "method" | "query" | "path">;

export interface ApiConfig<SecurityDataType = unknown> {
  baseUrl?: string;
  baseApiParams?: Omit<RequestParams, "baseUrl" | "cancelToken" | "signal">;
  securityWorker?: (securityData: SecurityDataType | null) => Promise<RequestParams | void> | RequestParams | void;
  customFetch?: typeof fetch;
}

export interface HttpResponse<D extends unknown, E extends unknown = unknown> extends Response {
  data: D;
  error: E;
}

type CancelToken = Symbol | string | number;

export enum ContentType {
  Json = "application/json",
  FormData = "multipart/form-data",
  UrlEncoded = "application/x-www-form-urlencoded",
  Text = "text/plain",
}

export class HttpClient<SecurityDataType = unknown> {
  public baseUrl: string = "";
  private securityData: SecurityDataType | null = null;
  private securityWorker?: ApiConfig<SecurityDataType>["securityWorker"];
  private abortControllers = new Map<CancelToken, AbortController>();
  private customFetch = (...fetchParams: Parameters<typeof fetch>) => fetch(...fetchParams);

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
    const keys = Object.keys(query).filter((key) => "undefined" !== typeof query[key]);
    return keys
      .map((key) => (Array.isArray(query[key]) ? this.addArrayQueryParam(query, key) : this.addQueryParam(query, key)))
      .join("&");
  }

  protected addQueryParams(rawQuery?: QueryParamsType): string {
    const queryString = this.toQueryString(rawQuery);
    return queryString ? `?${queryString}` : "";
  }

  private contentFormatters: Record<ContentType, (input: any) => any> = {
    [ContentType.Json]: (input: any) =>
      input !== null && (typeof input === "object" || typeof input === "string") ? JSON.stringify(input) : input,
    [ContentType.Text]: (input: any) => (input !== null && typeof input !== "string" ? JSON.stringify(input) : input),
    [ContentType.FormData]: (input: any) =>
      Object.keys(input || {}).reduce((formData, key) => {
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
      }, new FormData()),
    [ContentType.UrlEncoded]: (input: any) => this.toQueryString(input),
  };

  protected mergeRequestParams(params1: RequestParams, params2?: RequestParams): RequestParams {
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

  protected createAbortSignal = (cancelToken: CancelToken): AbortSignal | undefined => {
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

    return this.customFetch(`${baseUrl || this.baseUrl || ""}${path}${queryString ? `?${queryString}` : ""}`, {
      ...requestParams,
      headers: {
        ...(requestParams.headers || {}),
        ...(type && type !== ContentType.FormData ? { "Content-Type": type } : {}),
      },
      signal: (cancelToken ? this.createAbortSignal(cancelToken) : requestParams.signal) || null,
      body: typeof body === "undefined" || body === null ? null : payloadFormatter(body),
    }).then(async (response) => {
      const r = response.clone() as HttpResponse<T, E>;
      r.data = null as unknown as T;
      r.error = null as unknown as E;

      const data = !responseFormat
        ? r
        : await response[responseFormat]()
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
 * @title Mining Development Kit API
 * @version 1.0.0
 * @license MIT (https://www.mit.edu/~amini/LICENSE.md)
 * @baseUrl https://virtserver.swaggerhub.com/kkurucz/mining_development_kit_api/1.0.0
 * @contact <btcm-sw-team@squareup.com>
 *
 * The Mining Development Kit API serves as a means to access information from the mining device and make necessary adjustments to its settings.
 */
export class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
  api = {
    /**
     * @description The get pools endpoint returns the full list of currently configured pools.
     *
     * @tags Pools
     * @name ListPools
     * @request GET:/api/v1/pools
     */
    listPools: (params: RequestParams = {}) =>
      this.request<PoolsList, MessageResponse>({
        path: `/api/v1/pools`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The post pools endpoint allows up to three pools to be configured, replacing the previous pool configuration.
     *
     * @tags Pools
     * @name CreatePools
     * @request POST:/api/v1/pools
     * @secure
     */
    createPools: (data: PoolConfig, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/pools`,
        method: "POST",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Pools
     * @name GetPool
     * @request GET:/api/v1/pools/{id}
     */
    getPool: (id: number, params: RequestParams = {}) =>
      this.request<PoolResponse, MessageResponse>({
        path: `/api/v1/pools/${id}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Using this pool configuration endpoint, users can edit the properties of an existing pool.
     *
     * @tags Pools
     * @name EditPool
     * @request PUT:/api/v1/pools/{id}
     * @secure
     */
    editPool: (id: number, data: PoolConfigInner, params: RequestParams = {}) =>
      this.request<PoolConfigResponse, MessageResponse>({
        path: `/api/v1/pools/${id}`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @tags Pools
     * @name DeletePool
     * @request DELETE:/api/v1/pools/{id}
     * @secure
     */
    deletePool: (id: number, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/pools/${id}`,
        method: "DELETE",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description Used to test a pool connection
     *
     * @tags Pools
     * @name TestPoolConnection
     * @request POST:/api/v1/pools/test-connection
     */
    testPoolConnection: (data: TestConnection, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/pools/test-connection`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The password endpoint allows users to set a password during onboarding
     *
     * @tags Authentication
     * @name SetPassword
     * @request PUT:/api/v1/auth/password
     */
    setPassword: (data: PasswordRequest, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/auth/password`,
        method: "PUT",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Authenticates a user using a password and returns a JWT access and refresh token pair.
     *
     * @tags Authentication
     * @name Login
     * @request POST:/api/v1/auth/login
     */
    login: (data: PasswordRequest, params: RequestParams = {}) =>
      this.request<AuthTokens, MessageResponse>({
        path: `/api/v1/auth/login`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Validates and blacklists JWT tokens, effectively logging out the user.
     *
     * @tags Authentication
     * @name V1AuthLogoutCreate
     * @summary User logout
     * @request POST:/api/v1/auth/logout
     * @secure
     */
    v1AuthLogoutCreate: (data: AuthTokens, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/auth/logout`,
        method: "POST",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Validates the provided refresh token and returns a new JWT access token.
     *
     * @tags Authentication
     * @name V1AuthRefreshCreate
     * @summary Refresh JWT access token
     * @request POST:/api/v1/auth/refresh
     */
    v1AuthRefreshCreate: (data: RefreshRequest, params: RequestParams = {}) =>
      this.request<RefreshResponse, MessageResponse>({
        path: `/api/v1/auth/refresh`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The system endpoint provides information related to the control board including OS, software, and hardware component details.
     *
     * @tags System
     * @name GetSystemInfo
     * @request GET:/api/v1/system
     */
    getSystemInfo: (params: RequestParams = {}) =>
      this.request<SystemInfo, any>({
        path: `/api/v1/system`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Get system statuses
     *
     * @tags System Information
     * @name GetSystemStatus
     * @request GET:/api/v1/system/status
     */
    getSystemStatus: (params: RequestParams = {}) =>
      this.request<SystemStatuses, any>({
        path: `/api/v1/system/status`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The mining endpoint provides summary information about the mining operations of the device. This includes device level hashrate statistics, overall miner status, and current power usage and target information.
     *
     * @tags Mining
     * @name GetMiningStatus
     * @request GET:/api/v1/mining
     */
    getMiningStatus: (params: RequestParams = {}) =>
      this.request<MiningStatus, MessageResponse>({
        path: `/api/v1/mining`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The mining target endpoint returns the current power target in watts that the miner is controlling for.
     *
     * @tags Mining
     * @name GetMiningTarget
     * @request GET:/api/v1/mining/target
     */
    getMiningTarget: (params: RequestParams = {}) =>
      this.request<MiningTarget, MessageResponse>({
        path: `/api/v1/mining/target`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The mining target endpoint can be used to set a target power consumption for the miner. Once set, the mining device will operate to consume as close to that amount of power as possible. In the event that the device is unable to maintain its temperature within the allowed range, it may scale down and use less power.
     *
     * @tags Mining
     * @name EditMiningTarget
     * @request PUT:/api/v1/mining/target
     * @secure
     */
    editMiningTarget: (data: MiningTarget, params: RequestParams = {}) =>
      this.request<MiningTarget, MessageResponse>({
        path: `/api/v1/mining/target`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The start mining endpoint can be used to make the device start mining, into account the current power target of the system.
     *
     * @tags Mining
     * @name StartMining
     * @request POST:/api/v1/mining/start
     * @secure
     */
    startMining: (params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/mining/start`,
        method: "POST",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description The stop mining endpoint can be used to stop the device from mining, going into a minimal power mode with only the control board running.
     *
     * @tags Mining
     * @name StopMining
     * @request POST:/api/v1/mining/stop
     * @secure
     */
    stopMining: (params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/mining/stop`,
        method: "POST",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description The reboot endpoint can be used to reboot the entire system.
     *
     * @tags System
     * @name RebootSystem
     * @request POST:/api/v1/system/reboot
     * @secure
     */
    rebootSystem: (params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/system/reboot`,
        method: "POST",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description The locate system endpoint can be used to flash the indicator LED on the control board to assist in finding the miner.
     *
     * @tags System
     * @name LocateSystem
     * @request POST:/api/v1/system/locate
     * @secure
     */
    locateSystem: (
      query?: {
        /**
         * The duration in seconds for which to turn on the LED, with a max value of 300 seconds. If not specified, a default value of 30 seconds will be used. Requests made while the LED is on will be ignored.
         * @default 30
         */
        led_on_time?: number;
      },
      params: RequestParams = {},
    ) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/system/locate`,
        method: "POST",
        query: query,
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description The logs endpoint provides the most recent log lines from a given source, either OS, pool software, or miner logs.
     *
     * @tags System
     * @name GetSystemLogs
     * @request GET:/api/v1/system/logs
     */
    getSystemLogs: (
      query?: {
        /**
         * Number of log lines to return from the tail of the log, up to a maximum of 10000 lines. Defaults to 100 lines.
         * @default 100
         */
        lines?: number;
        /**
         * Source of logs to fetch. Defaults to miner software logs.
         * @default "miner_sw"
         * @example "miner_sw"
         */
        source?: "os" | "pool_sw" | "miner_sw" | "miner_web_server";
      },
      params: RequestParams = {},
    ) =>
      this.request<LogsResponse, MessageResponse>({
        path: `/api/v1/system/logs`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The update system endpoint can be used to initiate a system update of the miner software.
     *
     * @tags System
     * @name UpdateSystem
     * @request POST:/api/v1/system/update
     * @secure
     */
    updateSystem: (params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/system/update`,
        method: "POST",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description The get ssh endpoint returns if SSH is enabled or disabled on the control board
     *
     * @tags System
     * @name GetSsh
     * @request GET:/api/v1/system/ssh
     */
    getSsh: (params: RequestParams = {}) =>
      this.request<SshResponse, MessageResponse>({
        path: `/api/v1/system/ssh`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The put ssh endpoint enables/disables SSH on the control board
     *
     * @tags System
     * @name SetSsh
     * @request PUT:/api/v1/system/ssh
     * @secure
     */
    setSsh: (data: SshConfig, params: RequestParams = {}) =>
      this.request<SshResponse, MessageResponse>({
        path: `/api/v1/system/ssh`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The hashboards endpoint provides information about all of the hashboards connected to the system, including firmware version, MCU, ASIC count, API version, and hardware serial numbers.
     *
     * @tags Hashboards
     * @name GetAllHashboards
     * @request GET:/api/v1/hashboards
     */
    getAllHashboards: (params: RequestParams = {}) =>
      this.request<HashboardsInfo, MessageResponse>({
        path: `/api/v1/hashboards`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The hashboard status endpoint returns current operating statistics for a single hashboard in the system based on its serial number.
     *
     * @tags Hashboards
     * @name GetHashboardStatus
     * @request GET:/api/v1/hashboards/{hb_sn}
     */
    getHashboardStatus: (hbSn: string, params: RequestParams = {}) =>
      this.request<HashboardStats, MessageResponse>({
        path: `/api/v1/hashboards/${hbSn}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The hashboard logs endpoint provides the most recent log lines from the specified hashboard.
     *
     * @tags Hashboards
     * @name GetHashboardLogs
     * @request GET:/api/v1/hashboards/{hb_sn}/logs
     */
    getHashboardLogs: (
      hbSn: string,
      query?: {
        /**
         * The number of most recent logs to return. Maximum of 500, defaults to 100.
         * @default 100
         */
        lines?: number;
      },
      params: RequestParams = {},
    ) =>
      this.request<LogsResponse, MessageResponse>({
        path: `/api/v1/hashboards/${hbSn}/logs`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The hashboard status endpoint returns current operating statistics for a single ASIC on the specified hashboard in the system based on serial number and ASIC ID.
     *
     * @tags Hashboards
     * @name GetAsicStatus
     * @request GET:/api/v1/hashboards/{hb_sn}/{asic_id}
     */
    getAsicStatus: (hbSn: string, asicId: string, params: RequestParams = {}) =>
      this.request<AsicStatsResponse, MessageResponse>({
        path: `/api/v1/hashboards/${hbSn}/${asicId}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The hashrate endpoint provides miner-level historical hashrate operation data.
     *
     * @tags Hashrate
     * @name GetMinerHashrate
     * @request GET:/api/v1/hashrate
     */
    getMinerHashrate: (
      query?: {
        /** @default "12h" */
        duration?: "12h" | "24h" | "48h" | "5d";
      },
      params: RequestParams = {},
    ) =>
      this.request<HashrateResponse, MessageResponse>({
        path: `/api/v1/hashrate`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The hashrate endpoint provides hashboard-level historical operation data.
     *
     * @tags Hashrate
     * @name GetHashboardHashrate
     * @request GET:/api/v1/hashrate/{hb_sn}
     */
    getHashboardHashrate: (
      hbSn: string,
      query?: {
        /** @default "12h" */
        duration?: "12h" | "24h" | "48h" | "5d";
      },
      params: RequestParams = {},
    ) =>
      this.request<HashrateResponse, MessageResponse>({
        path: `/api/v1/hashrate/${hbSn}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The hashrate endpoint provides ASIC-level historical hashrate operation data.
     *
     * @tags Hashrate
     * @name GetAsicHashrate
     * @request GET:/api/v1/hashrate/{hb_sn}/{asic_id}
     */
    getAsicHashrate: (
      hbSn: string,
      asicId: number,
      query?: {
        /** @default "12h" */
        duration?: "12h" | "24h" | "48h" | "5d";
        /** @default "1m" */
        granularity?: "1m" | "5m" | "15m";
      },
      params: RequestParams = {},
    ) =>
      this.request<HashrateResponse, MessageResponse>({
        path: `/api/v1/hashrate/${hbSn}/${asicId}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The temperature endpoint provides miner-level historical temperature operation data.
     *
     * @tags Temperature
     * @name GetMinerTemperature
     * @request GET:/api/v1/temperature
     */
    getMinerTemperature: (
      query?: {
        /** @default "12h" */
        duration?: "12h" | "24h" | "48h" | "5d";
      },
      params: RequestParams = {},
    ) =>
      this.request<TemperatureResponse, MessageResponse>({
        path: `/api/v1/temperature`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The temperature endpoint provides hashboard-level historical operation data.
     *
     * @tags Temperature
     * @name GetHashboardTemperature
     * @request GET:/api/v1/temperature/{hb_sn}
     */
    getHashboardTemperature: (
      hbSn: string,
      query?: {
        /** @default "12h" */
        duration?: "12h" | "24h" | "48h" | "5d";
      },
      params: RequestParams = {},
    ) =>
      this.request<TemperatureResponse, MessageResponse>({
        path: `/api/v1/temperature/${hbSn}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The hashrate endpoint provides ASIC-level historical temperature operation data.
     *
     * @tags Temperature
     * @name GetAsicTemperature
     * @request GET:/api/v1/temperature/{hb_sn}/{asic_id}
     */
    getAsicTemperature: (
      hbSn: string,
      asicId: number,
      query?: {
        /** @default "12h" */
        duration?: "12h" | "24h" | "48h" | "5d";
        /** @default "1m" */
        granularity?: "1m" | "5m" | "15m";
      },
      params: RequestParams = {},
    ) =>
      this.request<TemperatureResponse, MessageResponse>({
        path: `/api/v1/temperature/${hbSn}/${asicId}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The power endpoint provides miner-level historical power operation data.
     *
     * @tags Power
     * @name GetMinerPower
     * @request GET:/api/v1/power
     */
    getMinerPower: (
      query?: {
        /** @default "12h" */
        duration?: "12h" | "24h" | "48h" | "5d";
      },
      params: RequestParams = {},
    ) =>
      this.request<PowerResponse, MessageResponse>({
        path: `/api/v1/power`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The power endpoint provides hashboard-level historical operation data.
     *
     * @tags Power
     * @name GetHashboardPower
     * @request GET:/api/v1/power/{hb_sn}
     */
    getHashboardPower: (
      hbSn: string,
      query?: {
        /** @default "12h" */
        duration?: "12h" | "24h" | "48h" | "5d";
      },
      params: RequestParams = {},
    ) =>
      this.request<PowerResponse, MessageResponse>({
        path: `/api/v1/power/${hbSn}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The efficiency endpoint provides miner-level historical power operation data.
     *
     * @tags Efficiency
     * @name GetMinerEfficiency
     * @request GET:/api/v1/efficiency
     */
    getMinerEfficiency: (
      query?: {
        /** @default "12h" */
        duration?: "12h" | "24h" | "48h" | "5d";
      },
      params: RequestParams = {},
    ) =>
      this.request<EfficiencyResponse, MessageResponse>({
        path: `/api/v1/efficiency`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The efficiency endpoint provides hashboard-level historical operation data.
     *
     * @tags Efficiency
     * @name GetHashboardEfficiency
     * @request GET:/api/v1/efficiency/{hb_sn}
     */
    getHashboardEfficiency: (
      hbSn: string,
      query?: {
        /** @default "12h" */
        duration?: "12h" | "24h" | "48h" | "5d";
      },
      params: RequestParams = {},
    ) =>
      this.request<EfficiencyResponse, MessageResponse>({
        path: `/api/v1/efficiency/${hbSn}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The cooling endpoint provides information on the cooling status of the device, including mode and current fan RPM.
     *
     * @tags Cooling
     * @name GetCooling
     * @request GET:/api/v1/cooling
     */
    getCooling: (params: RequestParams = {}) =>
      this.request<CoolingStatus, MessageResponse>({
        path: `/api/v1/cooling`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The cooling configuration endpoint allows the user to control the fan mode.
     *
     * @tags Cooling
     * @name SetCoolingMode
     * @request PUT:/api/v1/cooling
     * @secure
     */
    setCoolingMode: (data: CoolingConfig, params: RequestParams = {}) =>
      this.request<CoolingConfig, MessageResponse | ErrorResponse>({
        path: `/api/v1/cooling`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The network GET endpoint provides information related to the network configuration of the miner including IP address, gateways, and MAC address.
     *
     * @tags Network
     * @name GetNetwork
     * @request GET:/api/v1/network
     */
    getNetwork: (params: RequestParams = {}) =>
      this.request<NetworkInfo, MessageResponse>({
        path: `/api/v1/network`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The network PUT endpoint allows the user to change the configuration of the miner between DHCP and a static IP.
     *
     * @tags Network
     * @name SetNetworkConfig
     * @request PUT:/api/v1/network
     * @secure
     */
    setNetworkConfig: (data: NetworkConfig, params: RequestParams = {}) =>
      this.request<NetworkInfo, MessageResponse>({
        path: `/api/v1/network`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The errors endpoint provides alerts to be surfaced on the UI with different severity levels such as errors or warnings. This endpoint should be polled periodically to surface any issues that arise during mining operation.
     *
     * @tags Errors
     * @name GetErrors
     * @request GET:/api/v1/errors
     */
    getErrors: (params: RequestParams = {}) =>
      this.request<ErrorListResponse, MessageResponse>({
        path: `/api/v1/errors`,
        method: "GET",
        format: "json",
        ...params,
      }),
  };
}
