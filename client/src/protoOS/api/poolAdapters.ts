import { Pool, PoolConfigInner, TestConnection } from "./generatedApi";
import { isStratumV2URL } from "@/shared/components/MiningPools/PoolForm/constants";
import { PoolInfo } from "@/shared/components/MiningPools/types";

const authorityKeyForPool = (pool: PoolInfo) => (isStratumV2URL(pool.url) ? (pool.v2_authority_pubkey ?? "") : "");

export const apiPoolToPoolInfo = (pool: Pool | undefined, fallbackPriority: number): PoolInfo => ({
  name: pool?.name ?? "",
  url: pool?.url ?? "",
  username: pool?.user ?? "",
  password: "",
  priority: pool?.priority ?? fallbackPriority,
  v2_authority_pubkey: pool?.v2_authority_pubkey ?? "",
});

export const poolInfoToPoolConfig = (pool: PoolInfo): PoolConfigInner => ({
  name: pool.name,
  url: pool.url,
  username: pool.username,
  password: pool.password,
  priority: pool.priority,
  v2_authority_pubkey: authorityKeyForPool(pool),
});

export const poolInfoToTestConnection = (pool: PoolInfo): TestConnection => ({
  url: pool.url,
  username: pool.username,
  password: pool.password,
  v2_authority_pubkey: authorityKeyForPool(pool),
});
