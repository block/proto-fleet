import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useLocation, useParams } from "react-router-dom";

import TankCard from "./TankCard";
import TankMetrics from "./TankMetrics";
import { hashboardLabel, moduleHashboards } from "./tankView";
import type { TelemetryLevel } from "@/protoOS/api/adapters/types";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { asEntityKey, type ModuleId, type ModuleNode } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { findTankByRouteParam } from "@/protoOS/topologyRouteParams";
import Modal from "@/shared/components/Modal";

const TELEMETRY_LEVELS: TelemetryLevel[] = ["hashboard", "miner"];

const ModuleOverview = ({ moduleId, onDismiss }: { moduleId: ModuleId; onDismiss: () => void }) => {
  const { minerRoot } = useMinerHosting();
  const topology = useMinerStore((state) => state.hardware.topology);
  const rollup = useMinerStore((state) => state.telemetry.rollupsByEntity.get(asEntityKey(moduleId)));
  const module = topology?.modules.get(moduleId);
  if (!topology || !module) return null;
  const label = `Module ${module.location?.moduleSlot ?? module.id}`;
  return (
    <Modal title={label} dialogLabel={label} onDismiss={onDismiss}>
      <div>
        <p className="mb-6">{module.location?.tankLabel}</p>
        <TankMetrics rollup={rollup} />
        <div className="mt-6 flex flex-col gap-3">
          {moduleHashboards(module, topology).map((board) => (
            <Link
              key={board.id}
              to={`${minerRoot}/diagnostics/${encodeURIComponent(board.identity.value)}`}
              className="text-text-primary underline"
              onClick={onDismiss}
            >
              View hashboard ({hashboardLabel(board.location?.slot)})
            </Link>
          ))}
        </div>
      </div>
    </Modal>
  );
};

const Tanks = () => {
  const { tankId } = useParams();
  const { key: locationKey } = useLocation();
  const { error } = useTelemetry({ level: TELEMETRY_LEVELS });
  const topology = useMinerStore((state) => state.hardware.topology);
  const hardwareResource = useMinerStore((state) => state.hardware.resource);
  const [selectedModule, setSelectedModule] = useState<ModuleId>();
  const openModule = useCallback((module: ModuleNode) => setSelectedModule(module.id), []);
  const selectedTank = findTankByRouteParam(topology, tankId);
  const selectedTankId = selectedTank?.id;
  const tanks = useMemo(
    () =>
      topology?.kind === "container"
        ? [...topology.tanks.values()].sort((a, b) => (a.slot ?? Infinity) - (b.slot ?? Infinity))
        : [],
    [topology],
  );

  useEffect(() => {
    if (selectedTankId) document.getElementById(`tank-${selectedTankId}`)?.scrollIntoView({ block: "start" });
  }, [selectedTankId, locationKey]);

  if (!topology && (hardwareResource.status === "loading" || hardwareResource.status === "idle"))
    return <p role="status">Loading tanks…</p>;
  if (!topology || topology.kind !== "container") return <p role="alert">Unable to load tanks.</p>;

  return (
    <div className="flex flex-col gap-8" data-testid="container-tanks-page">
      <h1 className="sr-only">Tanks</h1>
      {error ? (
        <p role="alert" className="text-300 text-text-critical">
          Could not refresh tank readings. Displayed values may be out of date.
        </p>
      ) : null}
      {tankId && !selectedTank ? <p role="status">Tank not found. Showing all tanks.</p> : null}
      {tanks.length ? (
        tanks.map((tank, index) => (
          <TankCard
            key={tank.id}
            tank={tank}
            topology={topology}
            label={tank.label?.trim() || `Tank ${tank.slot ?? index + 1}`}
            onViewModule={openModule}
          />
        ))
      ) : (
        <p>No tanks found.</p>
      )}
      {selectedModule ? (
        <ModuleOverview moduleId={selectedModule} onDismiss={() => setSelectedModule(undefined)} />
      ) : null}
    </div>
  );
};

export default Tanks;
