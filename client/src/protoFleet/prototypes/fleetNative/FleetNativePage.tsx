/**
 * Strategy 1 — Fleet-native.
 *
 * Renders the shared <SingleMinerView> off a fleet snapshot. Wired to a mock
 * snapshot for now; the next step swaps `useFleetNativeSnapshot` to call the
 * throwaway `prototype/v1` Connect RPC (GetSingleMinerDetail).
 */
import { buildMockSnapshot } from "../shared/mockData";
import { SingleMinerView } from "../shared/SingleMinerView";

const FLEET_DATA_PATH = [
  { label: "ProtoFleet client", detail: "React" },
  { label: "Fleet server", detail: "Connect RPC" },
  { label: "proto plugin", detail: "normalizes" },
  { label: "Miner", detail: "MDK REST" },
];

export default function FleetNativePage() {
  const snapshot = buildMockSnapshot({
    name: "fleet-native-demo",
    source: "Fleet server (mock)",
    dataPath: FLEET_DATA_PATH,
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-md border border-dashed border-border-5 bg-surface-5 p-3 text-200 text-text-primary-50">
        Mock data. Next: back this with the <code>prototype/v1</code> Connect RPC that taps the plugin's normalized{" "}
        <code>GetDeviceMetrics</code> (incl. ASICs) — proving the fleet already collects this.
      </div>
      <SingleMinerView snapshot={snapshot} actions={{ onControl: (a) => console.warn(`(mock) ${a}`) }} />
    </div>
  );
}
