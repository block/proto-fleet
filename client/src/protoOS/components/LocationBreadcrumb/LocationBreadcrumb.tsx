import type { ContainerTopology, HashboardNode, ModuleId } from "@/protoOS/store/contract";
import Breadcrumb from "@/shared/components/Breadcrumb/Breadcrumb";

const LocationBreadcrumb = ({
  topology,
  moduleId,
  board,
}: {
  topology: ContainerTopology;
  moduleId: ModuleId;
  board?: HashboardNode;
}) => {
  const module = topology.modules.get(moduleId);
  const tank = module?.tankId ? topology.tanks.get(module.tankId) : undefined;
  if (!module || !tank || (board && board.moduleId !== module.id)) return null;

  // Use slot order for the slotless-tank label fallback.
  const tanks = [...topology.tanks.values()].sort((a, b) => (a.slot ?? Infinity) - (b.slot ?? Infinity));
  const tankLabel = tank.label?.trim() || `Tank ${tank.slot ?? tanks.findIndex((item) => item.id === tank.id) + 1}`;

  return (
    <>
      <Breadcrumb
        className="min-w-0 flex-wrap !text-heading-200 [overflow-wrap:anywhere] [&>span]:text-text-primary-50"
        segments={[{ label: tankLabel }, ...(board ? [{ label: `Module ${module.location?.moduleSlot ?? "?"}` }] : [])]}
      />
      <span aria-hidden="true" className="shrink-0 text-heading-200 text-text-primary-50">
        /
      </span>
    </>
  );
};

export default LocationBreadcrumb;
