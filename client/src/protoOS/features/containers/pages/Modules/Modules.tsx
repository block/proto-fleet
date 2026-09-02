import { useMemo } from "react";

import ContainerInventoryPage from "@/protoOS/features/containers/components/ContainerInventoryPage";
import useMinerStore from "@/protoOS/store/useMinerStore";

const Modules = () => {
  const topology = useMinerStore((state) => state.hardware.topology);
  const items = useMemo(() => {
    if (topology?.kind !== "container") return [];
    return [...topology.modules.values()]
      .sort(
        (a, b) =>
          (a.location?.moduleSlot ?? Number.MAX_SAFE_INTEGER) - (b.location?.moduleSlot ?? Number.MAX_SAFE_INTEGER),
      )
      .map((module, index) => ({
        id: String(module.id),
        label: `Module ${module.location?.moduleSlot ?? index + 1}`,
        details: [module.location?.tankLabel || "Unknown tank", `${module.hashboardIds.length} hashboards`],
      }));
  }, [topology]);

  return <ContainerInventoryPage title="Modules" items={items} emptyMessage="No modules found." />;
};

export default Modules;
