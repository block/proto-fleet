import { Pool } from "apiTypes";

export interface PoolInfo extends Pick<Pool, "status" | "url"> {
  index: number;
}
