/**
 * Shared polling interval for refreshing data across all ProtoFleet pages.
 * Can be overridden via VITE_POLL_INTERVAL_MS environment variable.
 * Default: 60 seconds
 */
export const POLL_INTERVAL_MS = Number(import.meta.env.VITE_POLL_INTERVAL_MS) || 60000;
