/**
 * URL contract for the rack-list building filter.
 *
 * Mirrors the singular `building` key shipped by the miner-list
 * filterUrlParams.ts (and the link emitted by BuildingPageHeader). Kept
 * here so RacksPage state and any future deep-link callers reuse one
 * URL key.
 */
export const BUILDING_URL_PARAM = "building";
