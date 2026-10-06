/**
 * URL contract for the rack-list building filter.
 *
 * Mirrors the singular `building` key shipped by the miner-list
 * filterUrlParams.ts (and the link emitted by BuildingPageHeader). Kept
 * here so RacksPage state and any future deep-link callers reuse one
 * parser and one URL key.
 */
export const BUILDING_URL_PARAM = "building";

/**
 * Read `building=<id>` (or `building=<id1>,<id2>`) from URLSearchParams and
 * return only the well-formed bigint values. Accepts both repeated-key
 * (`?building=1&building=2`) and legacy comma-joined (`?building=1,2`)
 * forms to mirror the `getMultiLegacy` parsing used by the miner-list
 * filterUrlParams.ts; old bookmarks keep working.
 */
export function parseBuildingIdsFromParams(params: URLSearchParams): bigint[] {
  return params
    .getAll(BUILDING_URL_PARAM)
    .flatMap((raw) => raw.split(","))
    .flatMap((raw) => {
      const trimmed = raw.trim();
      if (!trimmed || !/^\d+$/.test(trimmed)) return [];
      try {
        return [BigInt(trimmed)];
      } catch {
        return [];
      }
    });
}
