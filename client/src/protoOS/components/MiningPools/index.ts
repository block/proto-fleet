import MiningPools from "./MiningPools";
import { emptyPoolInfo, poolInfoAttributes } from "@/shared/components/MiningPools/constants";
import { getEmptyPoolsInfo, isValidPool } from "@/shared/components/MiningPools/utility";
import { PoolInfo } from "@/shared/types/pool";

export { emptyPoolInfo, type PoolInfo, poolInfoAttributes, getEmptyPoolsInfo, isValidPool };
export default MiningPools;
