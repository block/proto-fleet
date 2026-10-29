import { useMemo } from "react";
import { type Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { type FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";

interface NodePairingSummaryProps {
  miners: Device[];
  fleetNodes: FleetNodeItem[];
  showPairedCounts: boolean;
}

const NodePairingSummary = ({ miners, fleetNodes, showPairedCounts }: NodePairingSummaryProps) => {
  const sources = useMemo(() => {
    const counts = new Map<string, { selected: number; added: number }>();
    for (const miner of miners) {
      const id = miner.fleetNodeId.toString();
      const count = counts.get(id) ?? { selected: 0, added: 0 };
      count.selected++;
      if (!miner.boundToFleetNode) count.added++;
      counts.set(id, count);
    }
    const nodes = new Map(fleetNodes.map((node) => [node.fleetNodeId, node]));
    return [...counts].map(([id, { selected, added }]) => {
      const node = nodes.get(id);
      const paired = showPairedCounts && node ? node.pairedDeviceCount : undefined;
      return {
        id,
        name: id === "0" ? "Fleet Server" : (showPairedCounts && node?.name) || `Fleet Node ${id}`,
        paired,
        selected,
        added,
        total: paired === undefined ? undefined : paired + added,
      };
    });
  }, [miners, fleetNodes, showPairedCounts]);

  if (!sources.length) return null;
  return (
    <section className="mt-6 space-y-3" aria-label="Pairing by source">
      <h3 className="text-emphasis-300">Pairing by source</h3>
      <dl className="space-y-2 text-300">
        {sources.map(({ id, name, paired, selected, added, total }) => (
          <div key={id}>
            <div className="flex flex-wrap justify-between gap-x-4 gap-y-1">
              <dt>{name}</dt>
              <dd>
                {paired === undefined ? "Paired count unavailable" : `${paired.toLocaleString()} paired`}
                {selected === added
                  ? ` + ${selected.toLocaleString()} selected`
                  : ` + ${added.toLocaleString()} new (${selected.toLocaleString()} selected)`}
                {total === undefined ? "" : ` = ${total.toLocaleString()} total`}
              </dd>
            </div>
            {total !== undefined && total > 4000 ? (
              <Callout
                className="mt-2"
                intent="warning"
                prefixIcon={<Alert />}
                title={total > 5000 ? "Exceeds 5,000 miners per Fleet Node" : "Above the 4,000-miner target"}
              />
            ) : null}
          </div>
        ))}
      </dl>
      {sources.some(({ total }) => total !== undefined && total > 4000) ? (
        <p className="text-300 text-text-primary-70">Partition discovery ranges across Fleet Nodes before pairing.</p>
      ) : null}
    </section>
  );
};

export default NodePairingSummary;
