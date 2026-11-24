import type { PoolInfo } from "@/protoOS/store/contract";

export type PoolStatusEntry = Pick<PoolInfo, "status" | "url"> & Partial<Pick<PoolInfo, "priority">>;
