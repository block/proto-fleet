/**
 * Strategy 3 — Abstraction layer / adapters.
 *
 * One generic <SingleMinerView>, swappable backend adapters behind a single
 * `SingleMinerAdapter` seam. Pick an adapter context from the dropdown:
 *
 *   - Fleet server → the same distilled miners list as Strategy 2, but each row
 *     resolves through FleetAdapter (identity + KPIs from ListMinerStateSnapshots
 *     over /api-proxy; ASIC grid synthesized). This is a single-miner UI rendered
 *     *inside ProtoFleet off the fleet backend*.
 *   - MDK v1 miner (direct) → the same client served straight off a v1 fake rig,
 *     resolved through MdkV1Adapter.
 *   - MDK v2 miner (direct) → served straight off a v2 fake rig, through
 *     MdkV2Adapter.
 *
 * Every context folds its very different backend into the identical snapshot and
 * renders the same view — that's the whole thesis: same view code, different
 * adapters.
 */
import { useCallback, useEffect, useRef, useState } from "react";

import { FleetAdapter } from "../fleetNative/fleetAdapter";
import type { SingleMinerAdapter } from "../shared/adapter";
import { useFlowTrace } from "../shared/FlowPane";
import type { FlowTracer } from "../shared/flowTrace";
import { type MinerListItem, MinersList } from "../shared/MinersList";
import { MinerViewFrame } from "../shared/MinerViewFrame";
import { SingleMinerDetails } from "../shared/SingleMinerDetails";
import { SingleMinerView } from "../shared/SingleMinerView";
import type { MinerControlAction, MinerStatus, SingleMinerSnapshot } from "../shared/types";
import { MdkV1Adapter } from "./mdkV1Adapter";
import { MdkV2Adapter } from "./mdkV2Adapter";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import Button, { sizes as buttonSizes, variants as buttonVariants } from "@/shared/components/Button";
import Select from "@/shared/components/Select";

type Context = "fleet" | "mdkv1" | "mdkv2";

const CONTEXTS = [
  {
    value: "fleet",
    label: "Fleet server (Connect RPC)",
    description: "Rendered inside ProtoFleet off the fleet backend",
  },
  { value: "mdkv1", label: "MDK v1 miner (direct)", description: "Served straight off a v1 fake rig → MDK v1 adapter" },
  { value: "mdkv2", label: "MDK v2 miner (direct)", description: "Served straight off a v2 fake rig → MDK v2 adapter" },
];

const V1_URL = "http://localhost:18081";
const V2_URL = "http://localhost:18082";
const DEFAULT_PASSWORD = "admin1234";

// Single-miner view is only supported on first-party rigs (our proto rigs and
// the lab fake rigs). Filter out known third-party vendors the fleet may have
// discovered — we don't render the single-miner view for those.
const THIRD_PARTY_VENDORS = [
  "antminer",
  "bitmain",
  "whatsminer",
  "microbt",
  "avalon",
  "canaan",
  "innosilicon",
  "goldshell",
  "iceriver",
  "bitaxe",
];

function isFirstParty(model?: string, driver?: string, name?: string): boolean {
  const hay = `${model ?? ""} ${driver ?? ""} ${name ?? ""}`.toLowerCase();
  return !THIRD_PARTY_VENDORS.some((v) => hay.includes(v));
}

export default function AdapterPage() {
  const [context, setContext] = useState<Context>("fleet");
  const [fleetMiners, setFleetMiners] = useState<MinerListItem[] | null>(null);
  const [snapshot, setSnapshot] = useState<SingleMinerSnapshot | null>(null);
  const [connection, setConnection] = useState<SingleMinerAdapter | null>(null);
  const [busy, setBusy] = useState(true); // fleet list loads on mount
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const trace = useFlowTrace();

  const load = useCallback(async (adapter: SingleMinerAdapter, rowId: string | null, tracer?: FlowTracer) => {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setBusy(true);
    setBusyId(rowId);
    setError(null);
    try {
      setSnapshot(await adapter.fetchSnapshot(ctrl.signal, tracer));
      setConnection(adapter);
    } catch (e) {
      if (!ctrl.signal.aborted) setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (abortRef.current === ctrl) {
        setBusy(false);
        setBusyId(null);
      }
    }
  }, []);

  // Pure fetch → items, so the mount effect can populate the list without a
  // synchronous setState in the effect body. Single-miner view is only
  // supported on first-party (proto / lab) rigs, so third-party miners the
  // fleet has discovered are filtered out of the pick list.
  const fetchFleetMiners = useCallback(async (signal?: AbortSignal): Promise<MinerListItem[]> => {
    const res = await fleetManagementClient.listMinerStateSnapshots({ pageSize: 50 }, { signal });
    return res.miners
      .filter((m) => isFirstParty(m.model, m.driverName, m.name))
      .map((m) => {
        const hashrateThs = m.hashrate.length ? m.hashrate[0].value : null;
        const status: MinerStatus = (hashrateThs ?? 0) > 0 ? "mining" : "offline";
        return {
          id: m.ipAddress || m.deviceIdentifier,
          name: m.name || m.deviceIdentifier,
          ipAddress: m.ipAddress || undefined,
          firmware: m.firmwareVersion || undefined,
          status,
        };
      });
  }, []);

  const loadFleetList = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      setFleetMiners(await fetchFleetMiners());
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [fetchFleetMiners]);

  // A direct MDK context has a single fixed target, so selecting it is the
  // whole action — connect and render immediately, no intermediate click.
  const connectDirect = useCallback(
    (ctx: Context) => {
      const adapter = ctx === "mdkv2" ? new MdkV2Adapter(V2_URL) : new MdkV1Adapter(V1_URL, DEFAULT_PASSWORD);
      load(adapter, null, trace.makeTracer("direct"));
    },
    [load, trace],
  );

  const switchContext = useCallback(
    (next: Context) => {
      abortRef.current?.abort();
      // Switching prototype version is a fresh connection — clear the trace here.
      // (Retry, which calls connectDirect directly, appends instead.)
      trace.reset();
      setContext(next);
      setSnapshot(null);
      setConnection(null);
      setError(null);
      if (next === "fleet") loadFleetList();
      else connectDirect(next);
    },
    [loadFleetList, connectDirect, trace],
  );

  const openFleetMiner = useCallback(
    (item: MinerListItem) => {
      if (!item.ipAddress) {
        setError(`${item.name} has no IP to resolve.`);
        return;
      }
      trace.reset();
      load(new FleetAdapter(item.ipAddress), item.id, trace.makeTracer("fleet"));
    },
    [load, trace],
  );

  // A control action is a POST; append it (and the refetch) to the running trace
  // so the pane shows how writes are handled. No reset — the trace only clears
  // on a new connection or prototype switch. Only direct MDK v1 exposes controls.
  const runControl = useCallback(
    async (action: MinerControlAction) => {
      if (!connection?.control) return;
      const tracer = trace.makeTracer("direct");
      await connection.control(action, tracer);
      await load(connection, null, tracer);
    },
    [connection, load, trace],
  );

  const back = useCallback(() => {
    abortRef.current?.abort();
    setSnapshot(null);
    setConnection(null);
    setError(null);
  }, []);

  // Populate the fleet list on first mount (default context is fleet). State is
  // only set from async callbacks so the effect body stays synchronous-free.
  useEffect(() => {
    let active = true;
    fetchFleetMiners()
      .then((items) => active && setFleetMiners(items))
      .catch((e) => active && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => active && setBusy(false));
    return () => {
      active = false;
    };
  }, [fetchFleetMiners]);

  useEffect(() => () => abortRef.current?.abort(), []);

  return (
    <div className="flex flex-col gap-4">
      <Select
        id="adapter-context"
        label="Adapter context"
        options={CONTEXTS}
        value={context}
        onChange={(v) => switchContext(v as Context)}
      />

      {snapshot ? (
        <MinerViewFrame
          title={`Single-miner view · ${connection?.source ?? ""}`}
          // Fleet has a list to step back to; direct contexts are driven by the
          // dropdown, so there's nothing to go "back" to.
          leftAction={context === "fleet" ? { label: "Back", onClick: back } : undefined}
          details={<SingleMinerDetails snapshot={snapshot} />}
        >
          <SingleMinerView snapshot={snapshot} actions={connection?.control ? { onControl: runControl } : {}} />
        </MinerViewFrame>
      ) : context === "fleet" ? (
        <MinersList
          title="Fleet miners (first-party rigs)"
          items={fleetMiners ?? []}
          onSelect={openFleetMiner}
          busyId={busyId}
          emptyMessage={
            busy
              ? "Loading fleet miners…"
              : "No first-party rigs discovered — single-miner view isn't supported on third-party miners."
          }
        />
      ) : (
        <div className="flex flex-col items-center gap-4 rounded-xl border border-dashed border-border-5 bg-surface-elevated-base p-10 text-center">
          <div className="flex flex-col gap-1">
            <span className="text-heading-200 text-text-primary">
              {context === "mdkv2" ? "MDK v2 miner (direct)" : "MDK v1 miner (direct)"}
            </span>
            <span className="text-200 text-text-primary-50">
              {busy
                ? `Connecting to the ${context === "mdkv2" ? "v2" : "v1"} fake rig…`
                : `Couldn't reach the ${context === "mdkv2" ? "v2" : "v1"} fake rig. Is it running?`}
            </span>
          </div>
          {busy ? null : (
            <Button
              text="Retry"
              onClick={() => connectDirect(context)}
              size={buttonSizes.base}
              variant={buttonVariants.secondary}
            />
          )}
        </div>
      )}

      {error ? (
        <div className="rounded-lg border border-intent-critical-10 bg-intent-critical-10 p-3 text-200 text-text-critical">
          {error}
          <div className="mt-1 text-heading-100 text-text-primary-50">
            Direct MDK contexts need the fake rigs (<code>just lab-fakes</code>). Fleet needs fleet-api up and an
            authenticated session.
          </div>
        </div>
      ) : null}
    </div>
  );
}
