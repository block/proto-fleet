/**
 * Strategy 3 — Abstraction layer / adapters.
 *
 * Placeholder. Will expose a backend selector (fleet | mdkv1 | mdkv2); each
 * option resolves a SingleMinerAdapter that maps its backend into a
 * SingleMinerSnapshot, then renders the same shared <SingleMinerView>. The
 * mdkv1/mdkv2 adapters call the miner REST directly (no app server).
 */
export default function AdapterPage() {
  return (
    <div className="rounded-lg border border-dashed border-border-5 p-6 text-200 text-text-primary-50">
      Strategy 3 (adapter) — coming next. One generic view, swappable adapters for the fleet server and MDK v1 / v2
      miners.
    </div>
  );
}
