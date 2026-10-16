import { Link } from "react-router-dom";
import clsx from "clsx";
import ContainerActionsMenu from "./ContainerActionsMenu";
import { containerHealth } from "./containerHealth";
import { InventoryCell } from "./InventoryCell";
import { containerDetailPath } from "./useOpenContainerView";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { HEAT_CLASS } from "@/protoFleet/features/buildings/components/cardHealth";

export default function ContainerCard({
  container,
  inventory,
  onUnpaired,
}: {
  container: Container;
  inventory?: InventoryItem;
  onUnpaired?: (deviceIdentifier: string) => void;
}) {
  const { tanks: states, total, affected, incomplete } = containerHealth(container);
  const className = "flex h-full flex-col rounded-2xl bg-surface-overlay p-5";
  const content = (
    <>
      <div className="flex items-center justify-between gap-2 pr-10">
        <span className="truncate text-emphasis-300">{container.name}</span>
        <span className="text-200 text-text-primary-70">Container</span>
      </div>
      <div className="text-200 text-text-primary-70">{container.site?.label ?? "Unassigned"}</div>
      <div className="my-6 flex flex-wrap justify-center gap-1" aria-label="Tank health">
        {container.tanks.map((tank, index) => {
          const state = states[index];
          const summary = container.stale
            ? "Readings unavailable or stale"
            : `${state.affected} of ${state.total} modules affected${state.incomplete ? "; incomplete readings" : ""}`;
          return (
            <span
              key={tank.position}
              role="img"
              aria-label={`Tank ${tank.position}: ${summary}`}
              title={`Tank ${tank.position}: ${summary}`}
              data-heat-band={state.incomplete ? "unknown" : state.band}
              className={clsx(
                "h-8 w-4 rounded-sm",
                state.incomplete ? "border border-dashed border-border-5 bg-core-primary-5" : HEAT_CLASS[state.band],
              )}
            />
          );
        })}
      </div>
      <div className="mt-auto text-200 text-text-primary-70">
        {container.stale
          ? "Readings unavailable or stale"
          : incomplete
            ? "Incomplete module readings"
            : affected
              ? `${affected} of ${total} modules affected`
              : "All modules hashing"}
      </div>
      <div className="mt-3 flex justify-between gap-2 border-t border-border-5 pt-3 text-200">
        <span>
          {inventory ? (
            <InventoryCell item={inventory} column="hashrate" />
          ) : !container.stale && container.hashrateHs !== undefined ? (
            `${(container.hashrateHs / 1e12).toLocaleString(undefined, { maximumFractionDigits: 1 })} TH/s`
          ) : (
            "—"
          )}
        </span>
        <span>
          {inventory ? (
            <InventoryCell item={inventory} column="power" />
          ) : !container.stale && container.powerW !== undefined ? (
            `${(container.powerW / 1000).toLocaleString(undefined, { maximumFractionDigits: 1 })} kW`
          ) : (
            "—"
          )}
        </span>
      </div>
    </>
  );
  return (
    <div className="relative h-full">
      <Link
        className={`${className} transition-opacity hover:opacity-80`}
        data-testid={`container-card-${container.deviceIdentifier}`}
        to={containerDetailPath(container)}
      >
        {content}
      </Link>
      <div className="absolute top-4 right-5">
        <ContainerActionsMenu container={container} onUnpaired={onUnpaired} />
      </div>
    </div>
  );
}
