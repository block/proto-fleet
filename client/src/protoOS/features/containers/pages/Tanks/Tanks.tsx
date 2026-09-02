import { useMemo } from "react";

import ContainerInventoryPage from "@/protoOS/features/containers/components/ContainerInventoryPage";
import useMinerStore from "@/protoOS/store/useMinerStore";

const Tanks = () => {
  const topology = useMinerStore((state) => state.hardware.topology);
  const items = useMemo(() => {
    if (topology?.kind !== "container") return [];
    return [...topology.tanks.values()]
      .sort((a, b) => (a.slot ?? Number.MAX_SAFE_INTEGER) - (b.slot ?? Number.MAX_SAFE_INTEGER))
      .map((tank, index) => ({
        id: String(tank.id),
        label: tank.label?.trim() || `Tank ${tank.slot ?? index + 1}`,
        details: [
          `${tank.moduleIds.length} ${tank.moduleIds.length === 1 ? "module" : "modules"}`,
          `${tank.moduleIds.reduce(
            (count, moduleId) => count + (topology.modules.get(moduleId)?.hashboardIds.length ?? 0),
            0,
          )} hashboards`,
        ],
      }));
  }, [topology]);

  return <ContainerInventoryPage title="Tanks" items={items} emptyMessage="No tanks found." />;
};

export default Tanks;
