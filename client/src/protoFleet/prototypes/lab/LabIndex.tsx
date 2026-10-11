/** Landing page for the Prototype Lab — one card per strategy. */
import { Link } from "react-router-dom";

interface StrategyCard {
  path: string;
  title: string;
  pitch: string;
  dataPath: string;
  directToMiner: string;
}

const STRATEGIES: StrategyCard[] = [
  {
    path: "/lab/fleet-native",
    title: "1 · Fleet-native",
    pitch:
      "Fleet server collects & normalizes all data (via plugins). The view renders like any ProtoFleet component off a Connect RPC.",
    dataPath: "client → fleet server → plugin → miner",
    directToMiner: "Single-miner mode: a local ProtoFleet + app server you pair to one miner by IP.",
  },
  {
    path: "/lab/proxy",
    title: "2 · Proxy (versioned)",
    pitch:
      "Reverse-proxy to the miner (like today), but detect firmware/MDK version and render the matching per-version client.",
    dataPath: "client → fleet proxy → miner REST",
    directToMiner: "Renders the real per-version client against a live miner.",
  },
  {
    path: "/lab/adapter",
    title: "3 · Adapter",
    pitch:
      "One generic view, swappable backend adapters (fleet, MDK v1, MDK v2). Adapters can talk straight to a miner — no app server.",
    dataPath: "client → adapter → { fleet RPC | miner v1 REST | miner v2 REST }",
    directToMiner: "MDK v1/v2 adapters call the miner REST directly from the browser.",
  },
];

export default function LabIndex() {
  return (
    <div className="flex flex-col gap-4">
      <p className="text-200 text-text-primary-50">
        Three strategies for rendering a single-miner view, each against the same distilled surface (identity + 3 KPIs +
        hashboard/ASIC mini-grid + one control). The ASIC grid is the deliberate stressor — it's the one thing the fleet
        server doesn't expose today.
      </p>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        {STRATEGIES.map((s) => (
          <Link
            key={s.path}
            to={s.path}
            className="hover:border-emphasis-300 flex flex-col gap-3 rounded-lg border border-border-5 bg-surface-elevated-base p-4"
          >
            <span className="text-heading-200 text-text-primary">{s.title}</span>
            <span className="text-200 text-text-primary-70">{s.pitch}</span>
            <span className="rounded bg-surface-5 px-2 py-1 font-mono text-heading-100 text-text-primary-50">
              {s.dataPath}
            </span>
            <span className="text-heading-100 text-text-primary-50">{s.directToMiner}</span>
          </Link>
        ))}
      </div>
    </div>
  );
}
