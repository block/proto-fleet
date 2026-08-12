// Discovery `Device` carries only model + manufacturer (no device-type field yet),
// so a container is classified by its model prefix ("CU…") — the same convention
// as protoOS's useIsContainer. One-line swap once a real type flag lands.
const CONTAINER_MODEL_PREFIX = "CU";

/** True when a discovered device's model marks it as a container module (model "CU…"). */
export function isContainerModel(model: string): boolean {
  return typeof model === "string" && model.toUpperCase().startsWith(CONTAINER_MODEL_PREFIX);
}

/** Entity noun for a group's count, pluralized: "container(s)" for containers, "miner(s)" otherwise. */
export function entityNoun(model: string, count: number): string {
  if (isContainerModel(model)) return count === 1 ? "container" : "containers";
  return count === 1 ? "miner" : "miners";
}

/**
 * Display label for a discovered device group.
 * - Containers (model "CU…") are labelled simply "Container".
 * - Otherwise the manufacturer and model are joined ("Bitmain Antminer S21"),
 *   but a redundant manufacturer prefix is dropped when the model already leads
 *   with it as a whole word (manufacturer "Proto" + model "Proto Rig" -> "Proto Rig").
 */
export function deviceGroupLabel(manufacturer: string, model: string): string {
  if (isContainerModel(model)) return "Container";

  const mfr = (manufacturer ?? "").trim();
  const mdl = (model ?? "").trim();
  if (!mfr) return mdl;
  if (!mdl) return mfr;

  const mdlLower = mdl.toLowerCase();
  const mfrLower = mfr.toLowerCase();
  // Drop the prefix only on a whole-word match so "Proto" doesn't swallow "Protocol".
  if (mdlLower === mfrLower || mdlLower.startsWith(mfrLower + " ")) return mdl;

  return `${mfr} ${mdl}`;
}

/**
 * Header summary, splitting the total by entity, e.g.
 * "48 containers and 300 miners found on your network". A bucket with a zero
 * count is omitted, so a rig-only scan keeps the original
 * "N miners found on your network" wording.
 */
export function foundSummary(containerCount: number, minerCount: number): string {
  const parts: string[] = [];
  if (containerCount > 0) parts.push(`${containerCount} ${containerCount === 1 ? "container" : "containers"}`);
  if (minerCount > 0) parts.push(`${minerCount} ${minerCount === 1 ? "miner" : "miners"}`);
  return `${parts.join(" and ")} found on your network`;
}
