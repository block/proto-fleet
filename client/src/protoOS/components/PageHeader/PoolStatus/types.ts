import { Pool } from "@/protoOS/api/generatedApi";

export interface PoolInfo extends Pick<Pool, "status" | "url"> {
  index: number;
}
