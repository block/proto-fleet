import MiningPools from "./MiningPools";
import { emptyPoolInfo, info } from "@/shared/components/MiningPools/constants";
import { PoolInfo } from "@/shared/components/MiningPools/types";
import {
  getEmptyPoolsInfo,
  isValidPool,
} from "@/shared/components/MiningPools/utility";

export { emptyPoolInfo, type PoolInfo, info, getEmptyPoolsInfo, isValidPool };
export default MiningPools;
