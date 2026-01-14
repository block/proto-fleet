/**
 * Connection ID manager for stream deduplication.
 *
 * Generates a unique connection ID per browser tab. This ID is used by streaming
 * endpoints to allow multiple browser tabs to maintain independent streams while
 * still preventing duplicate streams within the same tab (e.g., from rapid scrolling).
 *
 * The server uses SessionID + ConnectionId as the deduplication key:
 * - Same session + same connection_id: New request cancels previous stream
 * - Same session + different connection_id: Both streams run independently (multi-tab)
 * - Different sessions: Streams run independently
 */

// Generate a unique ID once per tab lifecycle
const connectionId: string =
  typeof crypto !== "undefined" ? crypto.randomUUID() : `fallback-${Date.now()}-${Math.random()}`;

/**
 * Returns the unique connection ID for this browser tab.
 * The ID is generated once when this module is first loaded and remains
 * constant for the lifetime of the tab.
 */
export function getConnectionId(): string {
  return connectionId;
}
