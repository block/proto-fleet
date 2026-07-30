/**
 * Display metadata for a single miner, shared between the ProtoFleet shell
 * (single-miner route state) and the embedded ProtoOS hosting context. It is
 * app-neutral, so it lives in `shared/` and cannot drift between the two apps.
 */
export type MinerMetadata = {
  minerName?: string;
  ipAddress?: string;
  macAddress?: string;
  firmwareVersion?: string;
  // Fleet placement labels (site / building / rack). Only populated when the
  // miner is hosted inside the Fleet shell; standalone ProtoOS has no placement.
  site?: string;
  building?: string;
  rack?: string;
};
