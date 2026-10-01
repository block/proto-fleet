/**
 * The adapter seam shared by Strategy 1 (fleet-native) and Strategy 3.
 *
 * An adapter is the only backend-specific code in the abstraction strategy:
 * it knows how to reach one kind of backend (fleet server, MDK v1 REST, MDK v2
 * consolidated) and map it into the single `SingleMinerSnapshot` contract that
 * <SingleMinerView> renders. Everything downstream is identical.
 */
import type { FlowTracer } from "./flowTrace";
import type { MinerControlAction, SingleMinerSnapshot } from "./types";

export interface SingleMinerAdapter {
  /** Human label for the backend, surfaced in the data-path ribbon. */
  readonly source: string;
  /**
   * One read → one normalized snapshot. Pass a `tracer` to narrate the calls
   * and transforms into the data-flow pane (optional; defaults to no tracing).
   */
  fetchSnapshot(signal?: AbortSignal, tracer?: FlowTracer): Promise<SingleMinerSnapshot>;
  /**
   * Optional — not every backend exposes controls to the prototype. Pass a
   * `tracer` to narrate the POST into the data-flow pane.
   */
  control?(action: MinerControlAction, tracer?: FlowTracer): Promise<void>;
}
