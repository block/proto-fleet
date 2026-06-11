/**
 * Shared polling interval for refreshing data across all ProtoFleet pages.
 * Can be overridden via VITE_POLL_INTERVAL_MS environment variable.
 * Default: 15 seconds, chosen to roughly match the server's per-device
 * telemetry collection cadence (STALENESS_THRESHOLD) so the UI picks up
 * fresh rows shortly after they land.
 */
export const POLL_INTERVAL_MS = Number(import.meta.env.VITE_POLL_INTERVAL_MS) || 15000;
