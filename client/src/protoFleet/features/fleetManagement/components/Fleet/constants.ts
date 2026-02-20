/**
 * Polling interval for refreshing miner list data.
 * Can be overridden via VITE_MINER_LIST_POLL_INTERVAL_MS environment variable.
 * Default: 60 seconds
 */
export const POLL_INTERVAL_MS = Number(import.meta.env.VITE_MINER_LIST_POLL_INTERVAL_MS) || 60000;
