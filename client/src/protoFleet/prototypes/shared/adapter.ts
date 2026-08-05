/**
 * The adapter seam shared by Strategy 1 (fleet-native) and Strategy 3.
 *
 * An adapter is the only backend-specific code in the abstraction strategy:
 * it knows how to reach one kind of backend (fleet server, MDK v1 REST, MDK v2
 * consolidated) and map it into the single `SingleMinerSnapshot` contract that
 * <SingleMinerView> renders. Everything downstream is identical.
 */
import type { MinerControlAction, SingleMinerSnapshot } from "./types";

export interface SingleMinerAdapter {
  /** Human label for the backend, surfaced in the data-path ribbon. */
  readonly source: string;
  /** One read → one normalized snapshot. */
  fetchSnapshot(signal?: AbortSignal): Promise<SingleMinerSnapshot>;
  /** Optional — not every backend exposes controls to the prototype. */
  control?(action: MinerControlAction): Promise<void>;
}
