// ProtoOS durations (used by single miner dashboard)
export const durations = ["1h", "12h", "24h", "48h", "5d"] as const;

export type Duration = (typeof durations)[number];

// ProtoFleet durations
export const fleetDurations = ["1h", "24h", "3d", "10d", "30d", "90d", "1y"] as const;

export type FleetDuration = (typeof fleetDurations)[number];
