/**
 * Strategy 1 — Fleet-native single-miner view.
 *
 * The whole point is the *experience*: connect to a miner (IP + credentials),
 * and watch the single-miner view render — sourced entirely from the fleet
 * server via `ListMinerStateSnapshots` (over /api-proxy), never touching the
 * device. Because it's fleet-native it's self-explanatory: the same fleet
 * components you'd see anywhere in ProtoFleet.
 *
 * Identity + KPIs are real fleet data. The ASIC grid is synthesized — see
 * fleetAdapter.ts for exactly why (fleet collects components but discards them
 * at persistence; a `prototype/v1` RPC would make the grid real).
 */
import { useCallback, useEffect, useRef, useState } from "react";

import { useFlowTrace } from "../shared/FlowPane";
import { MinerViewFrame } from "../shared/MinerViewFrame";
import { SingleMinerDetails } from "../shared/SingleMinerDetails";
import { SingleMinerView } from "../shared/SingleMinerView";
import type { SingleMinerSnapshot } from "../shared/types";
import { FleetAdapter } from "./fleetAdapter";
import Button, { sizes as buttonSizes, variants as buttonVariants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";

export default function FleetNativePage() {
  const [ip, setIp] = useState("");
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("admin1234");
  const [snapshot, setSnapshot] = useState<SingleMinerSnapshot | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [connectOpen, setConnectOpen] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const trace = useFlowTrace();

  const connect = useCallback(async () => {
    if (!ip) return;
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setBusy(true);
    setError(null);
    trace.reset();
    try {
      // Fleet-native: credentials frame the "connect to a miner" experience;
      // the fleet already knows the device, so the RPC resolves it by IP.
      const snap = await new FleetAdapter(ip).fetchSnapshot(ctrl.signal, trace.makeTracer("fleet-native"));
      setSnapshot(snap);
      setConnectOpen(false);
    } catch (e) {
      if (!ctrl.signal.aborted) setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (abortRef.current === ctrl) setBusy(false);
    }
  }, [ip, trace]);

  useEffect(() => () => abortRef.current?.abort(), []);

  return (
    <>
      {snapshot ? (
        <MinerViewFrame
          title="Fleet-native single miner"
          leftAction={{ label: "Change miner", onClick: () => setConnectOpen(true) }}
          details={<SingleMinerDetails snapshot={snapshot} />}
        >
          <SingleMinerView snapshot={snapshot} />
        </MinerViewFrame>
      ) : (
        <div className="flex flex-col items-center gap-4 rounded-xl border border-dashed border-border-5 bg-surface-elevated-base p-10 text-center">
          <div className="flex flex-col gap-1">
            <span className="text-heading-200 text-text-primary">No miner connected</span>
            <span className="text-200 text-text-primary-50">
              Connect to a miner to render its single-miner view, fleet-native.
            </span>
          </div>
          <Button
            text="Connect a miner"
            onClick={() => setConnectOpen(true)}
            size={buttonSizes.base}
            variant={buttonVariants.primary}
          />
          {error ? <span className="text-200 text-text-critical">{error}</span> : null}
        </div>
      )}

      <Modal
        open={connectOpen}
        onDismiss={() => setConnectOpen(false)}
        title="Connect to a miner"
        size={modalSizes.standard}
        divider
        buttons={[
          { text: "Cancel", variant: buttonVariants.secondary, dismissModalOnClick: true },
          {
            text: "Connect",
            variant: buttonVariants.primary,
            onClick: connect,
            disabled: !ip || busy,
            loading: busy,
          },
        ]}
      >
        <div className="flex flex-col gap-3">
          <Input id="miner-ip" label="Miner IP" initValue={ip} onChange={(v) => setIp(v)} autoFocus />
          <Input id="miner-username" label="Username" initValue={username} onChange={(v) => setUsername(v)} />
          <Input
            id="miner-password"
            label="Password"
            type="password"
            initValue={password}
            onChange={(v) => setPassword(v)}
          />
          {error ? <span className="text-200 text-text-critical">{error}</span> : null}
        </div>
      </Modal>
    </>
  );
}
