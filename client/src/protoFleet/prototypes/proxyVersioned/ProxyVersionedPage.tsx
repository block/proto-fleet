/**
 * Strategy 2 — Proxy to miner, version-aware (the fleet-side experience).
 *
 * This is the ProtoFleet miners tab, dumbed down to two rigs running different
 * MDK versions. Click either and its single-miner view renders with requests
 * proxied straight to that miner — v1 and v2 both fold into the same snapshot
 * and render the identical <SingleMinerView>, matching strategies 1 and 3. The
 * firmware version only changes the *fetch* (probed via `/api/version`), never
 * the view.
 *
 * In the Lab the adapters hit the fake rigs directly; in production the calls
 * ride the minerproxy path (/api-proxy/miners/:id), so no browser CORS/TLS and
 * no direct device exposure.
 */
import { useCallback, useEffect, useRef, useState } from "react";

import { probeAndResolve } from "../adapter/probe";
import type { SingleMinerAdapter } from "../shared/adapter";
import { useFlowTrace } from "../shared/FlowPane";
import type { FlowTracer } from "../shared/flowTrace";
import { type MinerListItem, MinersList } from "../shared/MinersList";
import { MinerViewFrame } from "../shared/MinerViewFrame";
import { SingleMinerDetails } from "../shared/SingleMinerDetails";
import { SingleMinerView } from "../shared/SingleMinerView";
import type { MinerControlAction, SingleMinerSnapshot } from "../shared/types";

interface Rig extends MinerListItem {
  baseUrl: string;
  password: string;
}

const RIGS: Rig[] = [
  {
    id: "rig-01",
    name: "Proto Rig 01",
    mdkVersion: "MDK v1",
    ipAddress: "localhost:18081",
    firmware: "1.8.0",
    baseUrl: "http://localhost:18081",
    password: "admin1234",
  },
  {
    id: "rig-02",
    name: "Proto Rig 02",
    mdkVersion: "MDK v2",
    ipAddress: "localhost:18082",
    firmware: "1.4.2",
    baseUrl: "http://localhost:18082",
    password: "admin1234",
  },
];

export default function ProxyVersionedPage() {
  const [connection, setConnection] = useState<SingleMinerAdapter | null>(null);
  const [snapshot, setSnapshot] = useState<SingleMinerSnapshot | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const trace = useFlowTrace();

  const load = useCallback(async (adapter: SingleMinerAdapter, rowId: string | null, tracer?: FlowTracer) => {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setBusyId(rowId);
    setError(null);
    try {
      setSnapshot(await adapter.fetchSnapshot(ctrl.signal, tracer));
      setConnection(adapter);
    } catch (e) {
      if (!ctrl.signal.aborted) setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (abortRef.current === ctrl) setBusyId(null);
    }
  }, []);

  const openRig = useCallback(
    async (item: MinerListItem) => {
      const rig = RIGS.find((r) => r.id === item.id);
      if (!rig) return;
      setBusyId(rig.id);
      setError(null);
      trace.reset();
      const tracer = trace.makeTracer("proxy");
      try {
        const resolved = await probeAndResolve(rig.baseUrl, rig.password, undefined, tracer);
        await load(resolved.adapter, rig.id, tracer);
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
        setBusyId(null);
      }
    },
    [load, trace],
  );

  // A control action is a POST; append it (and the refetch that follows) to the
  // running trace so the pane shows writes riding the same proxy path as reads.
  // No reset — the trace only clears on a new connection or prototype switch.
  const runControl = useCallback(
    async (action: MinerControlAction) => {
      if (!connection?.control) return;
      const tracer = trace.makeTracer("proxy");
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

  useEffect(() => () => abortRef.current?.abort(), []);

  if (snapshot) {
    return (
      <MinerViewFrame
        title="Miner · proxied (version-aware)"
        leftAction={{ label: "Back to miners", onClick: back }}
        details={<SingleMinerDetails snapshot={snapshot} />}
      >
        <SingleMinerView snapshot={snapshot} actions={connection?.control ? { onControl: runControl } : {}} />
      </MinerViewFrame>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-200 text-text-primary-50">
        Two fake rigs on different MDK versions. Click one to open its single-miner view — requests proxy to the actual
        miner, and both versions render the same view.
      </p>
      <MinersList title="Miners" items={RIGS} onSelect={openRig} busyId={busyId} />
      {error ? (
        <div className="rounded-lg border border-intent-critical-10 bg-intent-critical-10 p-3 text-200 text-text-critical">
          {error}
          <div className="mt-1 text-heading-100 text-text-primary-50">
            Needs the fake rigs (<code>just lab-fakes</code>, v1 :18081 / v2 :18082).
          </div>
        </div>
      ) : null}
    </div>
  );
}
