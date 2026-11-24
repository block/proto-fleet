import { useMemo } from "react";

import type { Topology, ViewSelection } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

export const resolveKpiViewSelection = (topology: Topology | undefined): ViewSelection | undefined => {
  if (!topology) return undefined;
  if (topology.kind === "rig") return { level: "rig", moduleId: topology.rootModuleId };
  if (topology.kind === "directModule") return { level: "rig", moduleId: topology.moduleId };

  return { level: "container", containerId: topology.containerId };
};

export const useKpiViewSelection = () => {
  const topology = useMinerStore((state) => state.hardware.topology);

  return useMemo(() => resolveKpiViewSelection(topology), [topology]);
};
