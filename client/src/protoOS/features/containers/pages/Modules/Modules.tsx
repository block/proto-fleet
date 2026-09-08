import { useMemo } from "react";
import { useParams } from "react-router-dom";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import ContainerInventoryPage from "@/protoOS/features/containers/components/ContainerInventoryPage";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { decodeRouteParam, findTankByRouteParam } from "@/protoOS/topologyRouteParams";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Modules = () => {
  const navigate = useNavigate();
  const { minerRoot } = useMinerHosting();
  const { tankId: routeTankId } = useParams();
  const topology = useMinerStore((state) => state.hardware.topology);
  const tankRouteValue = decodeRouteParam(routeTankId);

  const selectedTank = useMemo(() => findTankByRouteParam(topology, tankRouteValue), [tankRouteValue, topology]);

  const items = useMemo(() => {
    if (topology?.kind !== "container") return [];
    if (tankRouteValue && !selectedTank) return [];

    const modules = selectedTank
      ? selectedTank.moduleIds.flatMap((moduleId) => {
          const module = topology.modules.get(moduleId);
          return module ? [module] : [];
        })
      : [...topology.modules.values()];

    return modules
      .sort(
        (a, b) =>
          (a.location?.tankLabel ?? "").localeCompare(b.location?.tankLabel ?? "") ||
          (a.location?.moduleSlot ?? Number.MAX_SAFE_INTEGER) - (b.location?.moduleSlot ?? Number.MAX_SAFE_INTEGER),
      )
      .map((module, index) => ({
        id: selectedTank ? String(module.location?.moduleSlot ?? index + 1) : String(module.id),
        label: `Module ${module.location?.moduleSlot ?? index + 1}`,
        details: [module.location?.tankLabel || "Unknown tank", `${module.hashboardIds.length} hashboards`],
      }));
  }, [selectedTank, tankRouteValue, topology]);

  const title = selectedTank?.label ? `${selectedTank.label} modules` : tankRouteValue ? "Tank modules" : "Modules";
  const handleSelectModule = selectedTank
    ? (id: string) => navigate(`${minerRoot}/tanks/${selectedTank.slot ?? tankRouteValue}/modules/${id}`)
    : undefined;

  return (
    <ContainerInventoryPage
      title={title}
      items={items}
      emptyMessage="No modules found."
      onSelectItem={handleSelectModule}
    />
  );
};

export default Modules;
