import { useMemo } from "react";
import { useParams } from "react-router-dom";

import type { Topology, ViewSelection } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { decodeRouteParam, findModuleInTankByRouteParam, findTankByRouteParam } from "@/protoOS/topologyRouteParams";

export type KpiViewSelection = ViewSelection | { level: "notFound" };

export const resolvedKpiViewSelection = (selection: KpiViewSelection | undefined): ViewSelection | undefined =>
  selection?.level === "notFound" ? undefined : selection;

export const resolveKpiViewSelection = (
  topology: Topology | undefined,
  tankRouteValue: string | undefined,
  moduleRouteValue: string | undefined,
): KpiViewSelection | undefined => {
  if (!topology) return undefined;
  if (topology.kind === "rig") return { level: "rig", moduleId: topology.rootModuleId };
  if (topology.kind === "directModule") return { level: "rig", moduleId: topology.moduleId };

  if (!tankRouteValue) {
    return { level: "container", containerId: topology.containerId };
  }

  const tank = findTankByRouteParam(topology, tankRouteValue);
  if (!tank) return { level: "notFound" };
  if (!moduleRouteValue) return { level: "tank", tankId: tank.id };

  const module = findModuleInTankByRouteParam(topology, tank, moduleRouteValue);

  return module ? { level: "module", moduleId: module.id } : { level: "notFound" };
};

export const useKpiViewSelection = () => {
  const { moduleId, tankId } = useParams();
  const topology = useMinerStore((state) => state.hardware.topology);
  const tankRouteValue = decodeRouteParam(tankId);
  const moduleRouteValue = decodeRouteParam(moduleId);

  return useMemo(
    () => resolveKpiViewSelection(topology, tankRouteValue, moduleRouteValue),
    [moduleRouteValue, tankRouteValue, topology],
  );
};
