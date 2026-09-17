import { memo, useMemo, useState } from "react";
import { Link } from "react-router-dom";

import TankMetrics from "./TankMetrics";
import TankModuleGrid, { StatusMarker } from "./TankModuleGrid";
import { moduleStatus, moduleStatuses, statusLabels, tankMiningScope, tankModules } from "./tankView";
import { useTankPower } from "./useTankPower";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { asEntityKey, type ContainerTopology, type ModuleNode, type TankNode } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { tankRouteValue } from "@/protoOS/topologyRouteParams";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Switch from "@/shared/components/Switch";

interface Props {
  tank: TankNode;
  topology: ContainerTopology;
  label: string;
  onViewModule: (module: ModuleNode) => void;
}

const TankCard = memo(({ tank, topology, label, onViewModule }: Props) => {
  const { minerRoot } = useMinerHosting();
  const rollup = useMinerStore((state) => state.telemetry.rollupsByEntity.get(asEntityKey(tank.id)));
  const telemetry = useMinerStore((state) => state.telemetry.hashboards);
  const plant = useMinerStore((state) => {
    const plantId = topology.container.plantComponentIds.find((id) => state.hardware.plant.get(id)?.kind === "plant");
    return plantId ? state.telemetry.plant.get(plantId) : undefined;
  });
  const modules = useMemo(() => tankModules(tank, topology), [tank, topology]);
  const scope = useMemo(() => tankMiningScope(tank, modules), [tank, modules]);
  const statuses = useMemo(() => {
    const counts = { attention: 0, offline: 0, isolated: 0, sleeping: 0, healthy: 0, unknown: 0 };
    for (const module of modules) {
      const boards = module.hashboardIds.map((id) => telemetry.get(id));
      counts[moduleStatus(boards)]++;
    }
    return counts;
  }, [modules, telemetry]);
  const { pending, error, setPower, supported } = useTankPower();
  const [confirmOn, setConfirmOn] = useState<boolean>();
  const known = rollup?.miningStatus !== undefined && rollup.miningStatus !== "unknown";
  const on =
    rollup?.miningStatus === "mining" ||
    rollup?.miningStatus === "degradedMining" ||
    rollup?.miningStatus === "poweringOn";
  const transitioning = rollup?.miningStatus === "poweringOn" || rollup?.miningStatus === "poweringOff";
  let powerLabel = on ? "On" : "Off";
  if (pending || transitioning) {
    powerLabel = "Updating…";
  } else if (!known) {
    powerLabel = "Unknown";
  }

  return (
    <section id={`tank-${tank.id}`} aria-label={label} className="scroll-mt-6">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h2 className="text-heading-300">
          <Link aria-label={`View ${label}`} to={`${minerRoot}/tanks/${tankRouteValue(tank)}`}>
            {label}
          </Link>
        </h2>
        <Switch
          ariaLabel={`${label} power`}
          label={powerLabel}
          checked={on}
          disabled={
            !supported || tank.capabilities.startStop !== "supported" || !scope || pending || transitioning || !known
          }
          setChecked={() => setConfirmOn(!on)}
        />
      </div>
      {error ? (
        <p role="alert" className="mb-4 text-300 text-text-critical">
          {error}
        </p>
      ) : null}
      <div className="rounded-2xl border border-border-5 bg-surface-base shadow-100">
        <div className="p-6 laptop:p-11">
          <TankMetrics rollup={rollup} showCooling plant={plant} tankSlot={tank.slot} />
        </div>
        <div className="grid gap-8 border-t border-border-5 p-6 laptop:p-11 desktop:grid-cols-[minmax(0,3fr)_minmax(240px,2fr)] desktop:gap-16">
          {modules.length ? (
            <TankModuleGrid
              tank={tank}
              topology={topology}
              modules={modules}
              telemetry={telemetry}
              onViewModule={onViewModule}
            />
          ) : (
            <p className="text-300 text-text-primary-70">No modules found.</p>
          )}
          <dl className="flex flex-col justify-center divide-y divide-border-10">
            {moduleStatuses
              .filter((status) => status !== "unknown" || statuses.unknown > 0)
              .map((status) => (
                <div key={status} className="flex items-center justify-between gap-4 px-4 py-6">
                  <dt className="flex items-center gap-4 text-300">
                    <StatusMarker status={status} />
                    {statusLabels[status]}
                  </dt>
                  <dd className="text-300 whitespace-nowrap text-text-primary-70">
                    {statuses[status]} {statuses[status] === 1 ? "module" : "modules"}
                  </dd>
                </div>
              ))}
          </dl>
        </div>
      </div>
      {confirmOn !== undefined ? (
        <Dialog
          title={`${confirmOn ? "Turn on" : "Turn off"} ${label}?`}
          subtitle={`This will ${confirmOn ? "start" : "stop"} mining on ${modules.length} ${modules.length === 1 ? "module" : "modules"} in ${label}.`}
          onDismiss={() => setConfirmOn(undefined)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setConfirmOn(undefined) },
            {
              text: confirmOn ? "Turn on" : "Turn off",
              variant: variants.primary,
              onClick: () => {
                setConfirmOn(undefined);
                void setPower(scope, confirmOn);
              },
            },
          ]}
        />
      ) : null}
    </section>
  );
});

export default TankCard;
