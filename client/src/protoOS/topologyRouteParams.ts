import type { ModuleNode, TankNode, Topology } from "@/protoOS/store/contract";

export const decodeRouteParam = (value: string | undefined) => (value ? decodeURIComponent(value) : undefined);

const tankMatchesRoute = (tank: TankNode, value: string) => String(tank.id) === value || String(tank.slot) === value;

const moduleMatchesRoute = (module: ModuleNode, value: string, fallbackSlot: number) =>
  String(module.id) === value || String(module.location?.moduleSlot ?? fallbackSlot) === value;

export const findTankByRouteParam = (topology: Topology | undefined, routeValue: string | undefined) => {
  if (topology?.kind !== "container" || !routeValue) return undefined;
  return [...topology.tanks.values()].find((tank) => tankMatchesRoute(tank, routeValue));
};

export const findModuleInTankByRouteParam = (topology: Topology, tank: TankNode, routeValue: string | undefined) => {
  if (!routeValue) return undefined;

  const modules = tank.moduleIds.flatMap((moduleId) => {
    const module = topology.modules.get(moduleId);
    return module ? [module] : [];
  });

  return modules.find((module, index) => moduleMatchesRoute(module, routeValue, index + 1));
};
