import MiningPools from "./MiningPools";
import { emptyPoolInfo, poolInfoAttributes } from "@/shared/components/MiningPools/constants";
import { PoolInfo } from "@/shared/components/MiningPools/types";
import { getEmptyPoolsInfo, isValidPool } from "@/shared/components/MiningPools/utility";

export { emptyPoolInfo, type PoolInfo, poolInfoAttributes, getEmptyPoolsInfo, isValidPool };
export default MiningPools;
