import { runMiningAction } from "./runMiningAction";
export function runTankMiningAction(
  tankIds: bigint[],
  mining: boolean,
  onRefresh: () => void,
  onActionComplete?: () => void,
) {
  return runMiningAction(tankIds, "tank", mining, onRefresh, onActionComplete);
}
