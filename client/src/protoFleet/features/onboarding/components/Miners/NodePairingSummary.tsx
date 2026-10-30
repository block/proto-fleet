import { useMemo } from "react";
import {
  FLEET_SERVER_ID,
  fleetSourceLabel,
  MAX_MINERS_PER_NODE,
  RECOMMENDED_MINERS_PER_NODE,
} from "./foundMinersLabels";
import { type Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { type FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";

interface NodePairingSummaryProps {
  miners: Device[];
  fleetNodes: FleetNodeItem[];
}

const NodePairingSummary = ({ miners, fleetNodes }: NodePairingSummaryProps) => {
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
      const paired = node?.pairedDeviceCount;
      return {
        id,
        name: fleetSourceLabel(id, node?.name),
        pairedText:
          id === FLEET_SERVER_ID
            ? undefined
            : paired === undefined
              ? "Paired count unavailable"
              : `${paired.toLocaleString()} paired`,
        selected,
        added,
        total: paired === undefined ? undefined : paired + added,
      };
    });
  }, [miners, fleetNodes]);

  if (!sources.some(({ id }) => id !== FLEET_SERVER_ID)) return null;
  return (
    <section className="mt-6 space-y-3" aria-label="Pairing by source">
      <h3 className="text-emphasis-300">Pairing by source</h3>
      <dl className="space-y-2 text-300">
        {sources.map(({ id, name, pairedText, selected, added, total }) => (
          <div key={id}>
            <div className="flex flex-wrap justify-between gap-x-4 gap-y-1">
              <dt>{name}</dt>
              <dd>
                {pairedText === undefined ? "" : `${pairedText} + `}
                {selected === added
                  ? `${selected.toLocaleString()} selected`
                  : `${added.toLocaleString()} new (${selected.toLocaleString()} selected)`}
                {total === undefined ? "" : ` = ${total.toLocaleString()} total`}
              </dd>
            </div>
            {total !== undefined && total > RECOMMENDED_MINERS_PER_NODE ? (
              <Callout
                className="mt-2"
                intent="warning"
                prefixIcon={<Alert />}
                title={
                  total > MAX_MINERS_PER_NODE
                    ? `Exceeds ${MAX_MINERS_PER_NODE.toLocaleString()} miners per Fleet Node`
                    : `Above the ${RECOMMENDED_MINERS_PER_NODE.toLocaleString()}-miner target`
                }
              />
            ) : null}
          </div>
        ))}
      </dl>
      {sources.some(({ total }) => total !== undefined && total > RECOMMENDED_MINERS_PER_NODE) ? (
        <p className="text-300 text-text-primary-70">Partition discovery ranges across Fleet Nodes before pairing.</p>
      ) : null}
    </section>
  );
};

export default NodePairingSummary;
