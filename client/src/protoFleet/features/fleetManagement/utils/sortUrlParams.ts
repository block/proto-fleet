import { create } from "@bufbuild/protobuf";
import {
  type SortConfig,
  SortConfigSchema,
  SortDirection,
  SortField,
} from "@/protoFleet/api/generated/common/v1/sort_pb";
import { SORT_ASC, SORT_DESC } from "@/shared/components/List/types";

/**
 * URL parameter keys for sort state
 */
const URL_PARAMS = {
  SORT: "sort",
  DIR: "dir",
} as const;

/**
 * Maps URL field values to SortField enum.
 * Keys are lowercase for case-insensitive parsing.
 */
const URL_TO_SORT_FIELD: Record<string, SortField> = {
  name: SortField.NAME,
  "worker-name": SortField.WORKER_NAME,
  ip: SortField.IP_ADDRESS,
  mac: SortField.MAC_ADDRESS,
  model: SortField.MODEL,
  hashrate: SortField.HASHRATE,
  temp: SortField.TEMPERATURE,
  power: SortField.POWER,
  efficiency: SortField.EFFICIENCY,
  firmware: SortField.FIRMWARE,
};

/**
 * Maps SortField enum to URL field values.
 * Excludes UNSPECIFIED since that means no sort.
 */
const SORT_FIELD_TO_URL: Partial<Record<SortField, string>> = {
  [SortField.NAME]: "name",
  [SortField.WORKER_NAME]: "worker-name",
  [SortField.IP_ADDRESS]: "ip",
  [SortField.MAC_ADDRESS]: "mac",
  [SortField.MODEL]: "model",
  [SortField.HASHRATE]: "hashrate",
  [SortField.TEMPERATURE]: "temp",
  [SortField.POWER]: "power",
  [SortField.EFFICIENCY]: "efficiency",
  [SortField.FIRMWARE]: "firmware",
};

/**
 * Parses sort configuration from URL search parameters.
 * Returns undefined if no valid sort params are present.
 *
 * @example
 * // URL: ?sort=hashrate&dir=desc
 * parseSortFromURL(params) // MinerSortConfig { field: HASHRATE, direction: DESC }
 */
export function parseSortFromURL(params: URLSearchParams): SortConfig | undefined {
  const sortParam = params.get(URL_PARAMS.SORT);
  if (!sortParam) {
    return undefined;
  }

  const field = URL_TO_SORT_FIELD[sortParam.toLowerCase()];
  if (field === undefined) {
    console.warn(`Unknown sort field in URL: ${sortParam}`);
    return undefined;
  }

  const dirParam = params.get(URL_PARAMS.DIR);
  const direction = dirParam === SORT_ASC ? SortDirection.ASC : SortDirection.DESC;

  return create(SortConfigSchema, { field, direction });
}

/**
 * Encodes sort configuration to URL search parameters.
 * If sort is undefined or UNSPECIFIED, removes sort params from URL.
 *
 * @example
 * encodeSortToURL(params, { field: SortField.HASHRATE, direction: SortDirection.DESC })
 * // params now has: sort=hashrate&dir=desc
 */
export function encodeSortToURL(params: URLSearchParams, sort: SortConfig | undefined): void {
  if (!sort || sort.field === SortField.UNSPECIFIED) {
    params.delete(URL_PARAMS.SORT);
    params.delete(URL_PARAMS.DIR);
    return;
  }

  const urlField = SORT_FIELD_TO_URL[sort.field];
  if (!urlField) {
    console.warn(`No URL mapping for sort field: ${sort.field}`);
    return;
  }

  params.set(URL_PARAMS.SORT, urlField);
  params.set(URL_PARAMS.DIR, sort.direction === SortDirection.ASC ? SORT_ASC : SORT_DESC);
}
