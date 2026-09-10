import { useMemo } from "react";
import { useParams } from "react-router-dom";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import ContainerInventoryPage from "@/protoOS/features/containers/components/ContainerInventoryPage";
import useMinerStore from "@/protoOS/store/useMinerStore";
import {
  decodeRouteParam,
  findTankByRouteParam,
  moduleRouteValue,
  tankRouteValue,
} from "@/protoOS/topologyRouteParams";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Modules = () => {
  const navigate = useNavigate();
  const { minerRoot } = useMinerHosting();
  const { tankId: routeTankId } = useParams();
  const topology = useMinerStore((state) => state.hardware.topology);
  const decodedTankRouteValue = decodeRouteParam(routeTankId);

  const selectedTank = useMemo(
    () => findTankByRouteParam(topology, decodedTankRouteValue),
    [decodedTankRouteValue, topology],
  );

  const items = useMemo(() => {
    if (topology?.kind !== "container") return [];
    if (decodedTankRouteValue && !selectedTank) return [];

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
        id: selectedTank ? moduleRouteValue(module, index + 1) : String(module.id),
        label: `Module ${module.location?.moduleSlot ?? index + 1}`,
        details: [module.location?.tankLabel || "Unknown tank", `${module.hashboardIds.length} hashboards`],
      }));
  }, [decodedTankRouteValue, selectedTank, topology]);

  const title = selectedTank?.label
    ? `${selectedTank.label} modules`
    : decodedTankRouteValue
      ? "Tank modules"
      : "Modules";
  const handleSelectModule = selectedTank
    ? (id: string) => navigate(`${minerRoot}/tanks/${tankRouteValue(selectedTank)}/modules/${id}`)
    : undefined;
  const breadcrumbs = selectedTank
    ? [
        { label: "Home", to: `${minerRoot}/` },
        { label: "Tanks", to: `${minerRoot}/tanks` },
        { label: selectedTank.label?.trim() || title },
      ]
    : [];

  return (
    <ContainerInventoryPage
      breadcrumbs={breadcrumbs}
      title={title}
      items={items}
      emptyMessage="No modules found."
      onSelectItem={handleSelectModule}
    />
  );
};

export default Modules;
