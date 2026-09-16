import { useEffect, useMemo } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";

import ModuleModal from "./components/ModuleModal/ModuleModal";
import TankCard from "./TankCard";
import type { TelemetryLevel } from "@/protoOS/api/adapters/types";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { type ModuleNode } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";
import {
  findModuleInTankByRouteParam,
  findTankByRouteParam,
  moduleRouteValue,
  tankRouteValue,
} from "@/protoOS/topologyRouteParams";

const TELEMETRY_LEVELS: TelemetryLevel[] = ["hashboard", "miner"];

const Tanks = () => {
  const { tankId, moduleId } = useParams();
  const navigate = useNavigate();
  const { minerRoot } = useMinerHosting();
  const { key: locationKey } = useLocation();
  const { error } = useTelemetry({ level: TELEMETRY_LEVELS });
  const topology = useMinerStore((state) => state.hardware.topology);
  const hardwareResource = useMinerStore((state) => state.hardware.resource);
  const openModule = (module: ModuleNode) => {
    const tank = module.tankId && topology?.kind === "container" ? topology.tanks.get(module.tankId) : undefined;
    if (tank)
      navigate(
        `${minerRoot}/tanks/${tankRouteValue(tank)}/modules/${moduleRouteValue(module, tank.moduleIds.indexOf(module.id) + 1)}`,
      );
  };
  const selectedTank = findTankByRouteParam(topology, tankId);
  const selectedTankId = selectedTank?.id;
  const selectedModule =
    selectedTank && topology ? findModuleInTankByRouteParam(topology, selectedTank, moduleId) : undefined;
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
      {moduleId && selectedTank && !selectedModule ? <p role="status">Module not found. Showing tanks.</p> : null}
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
      {selectedModule && selectedTank ? (
        <ModuleModal module={selectedModule} tank={selectedTank} topology={topology} />
      ) : null}
    </div>
  );
};

export default Tanks;
