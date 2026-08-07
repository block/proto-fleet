/**
 * Data-flow tracing for the Lab.
 *
 * Each strategy narrates its own plumbing: as an adapter runs, it emits flow
 * events (API requests, adapter transforms, proxy hops) that the collapsible
 * <FlowPane> renders like a color-coded network tab. The colors encode *which
 * server the call ultimately targets*, so the three strategies read differently
 * at a glance even though they render the identical view.
 */

/** Which hop an event belongs to — drives its color. */
export type FlowChannel =
  | "smv-fleet" // ProtoFleet client → Fleet server (Connect RPC)
  | "fleet-miner" // Fleet server → miner (server-side telemetry collection)
  | "smv-miner" // ProtoFleet client → miner (REST, direct or via proxy)
  | "seam" // version-aware seam: probe fw, pick which client to render (S2, no network)
  | "adapter"; // adapter layer: map the generic data getter → backend API calls (S3, no network)

/** How the page reaches the miner — decides proxy annotation + which story to tell. */
export type Transport =
  | "fleet" // S3 fleet context: FleetAdapter maps the getter → fleet RPC (adapter row shown)
  | "fleet-native" // S1: the view reads the fleet proto directly, no adapter (row suppressed)
  | "proxy" // S2: version seam + proxied miner calls (adapter row suppressed)
  | "direct"; // S3 direct MDK contexts: adapter maps getter → REST (adapter row shown)

export interface FlowEvent {
  id: number;
  channel: FlowChannel;
  /** Primary line, e.g. "GET /api/v2/miner" or "Select MDK v2 adapter". */
  title: string;
  detail?: string;
  /** HTTP verb parsed from the title, when the event is a request. */
  method?: string;
  /** True when the miner call rides the fleet minerproxy. */
  proxied?: boolean;
  status: "pending" | "ok" | "error";
}

export interface FlowRequestHandle {
  ok(detail?: string): void;
  fail(message?: string): void;
}

/** The narrow surface adapters use to narrate themselves. */
export interface FlowTracer {
  /** A network call. `target` is the server it's ultimately bound for. */
  request(target: "fleet" | "miner", title: string, detail?: string): FlowRequestHandle;
  /** The version-aware seam: probe firmware, pick which client to render (S2). */
  seam(title: string, detail?: string): void;
  /** The adapter layer: map the view's generic data getter → backend API calls (S3). */
  adapter(title: string, detail?: string): void;
  /** A non-request annotation on a specific channel (e.g. server-side collection). */
  note(channel: FlowChannel, title: string, detail?: string): void;
}

/** No-op tracer so adapters can run untraced (tests, control refresh). */
export const NO_TRACE: FlowTracer = {
  request: () => ({ ok: () => {}, fail: () => {} }),
  seam: () => {},
  adapter: () => {},
  note: () => {},
};

export interface ChannelMeta {
  label: string;
  /** dot / bar background, text color, tint background utility classes. */
  dot: string;
  text: string;
  bar: string;
  tint: string;
}

export const CHANNEL_META: Record<FlowChannel, ChannelMeta> = {
  "smv-fleet": {
    label: "Client → Fleet server",
    dot: "bg-intent-info-fill",
    text: "text-intent-info-fill",
    bar: "border-intent-info-fill",
    tint: "bg-intent-info-10",
  },
  "fleet-miner": {
    label: "Fleet server → Miner (collection)",
    dot: "bg-[#8b5cf6]",
    text: "text-[#8b5cf6]",
    bar: "border-[#8b5cf6]",
    tint: "bg-[#8b5cf6]/10",
  },
  "smv-miner": {
    label: "Client → Miner (REST)",
    dot: "bg-intent-success-fill",
    text: "text-intent-success-fill",
    bar: "border-intent-success-fill",
    tint: "bg-intent-success-10",
  },
  seam: {
    label: "Version seam",
    dot: "bg-[#14b8a6]",
    text: "text-[#14b8a6]",
    bar: "border-[#14b8a6]",
    tint: "bg-[#14b8a6]/10",
  },
  adapter: {
    label: "Adapter layer",
    dot: "bg-intent-warning-fill",
    text: "text-intent-warning-fill",
    bar: "border-intent-warning-fill",
    tint: "bg-intent-warning-10",
  },
};
