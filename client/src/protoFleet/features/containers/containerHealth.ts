import type { Container, TankHealth } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { healthHeatBand } from "@/protoFleet/features/buildings/components/cardHealth";

export function tankHealth(tank: TankHealth, stale: boolean) {
  const affected = tank.attentionCount + tank.offlineCount + tank.sleepingCount + tank.isolatedCount;
  const total = tank.hashingCount + affected + tank.unknownCount;
  const incomplete = stale || tank.unknownCount > 0 || total === 0;
  return { affected, total, incomplete, band: healthHeatBand(total ? affected / total : 0) };
}

export function containerHealth(container: Container) {
  const tanks = container.tanks.map((tank) => tankHealth(tank, container.stale));
  const total = tanks.reduce((sum, tank) => sum + tank.total, 0);
  const affected = tanks.reduce((sum, tank) => sum + tank.affected, 0);
  const incomplete = container.stale || tanks.length === 0 || tanks.some((tank) => tank.incomplete);
  return { tanks, total, affected, incomplete };
}
