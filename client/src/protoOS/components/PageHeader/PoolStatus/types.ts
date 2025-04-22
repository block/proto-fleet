import { Pool } from "@/protoOS/api/types";

export interface PoolInfo extends Pick<Pool, "status" | "url"> {
  index: number;
}
