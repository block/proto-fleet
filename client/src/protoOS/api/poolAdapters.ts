import { PoolConfigInner, TestConnection } from "./adapters/mdkV1";
import { isStratumV2URL } from "@/shared/components/MiningPools/PoolForm/constants";
import { PoolInfo } from "@/shared/types/pool";

const authorityKeyForPool = (pool: PoolInfo) => (isStratumV2URL(pool.url) ? (pool.v2_authority_pubkey ?? "") : "");

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
