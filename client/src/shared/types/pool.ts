/**
 * Canonical mining-pool domain types. These describe the pool fields the UI
 * collects and the adapters accept — deliberately generation-agnostic and free
 * of any component or firmware coupling, so both the shared pool form and the
 * API adapters can depend on them without pointing "up" into UI code.
 */
export interface PoolInfo {
  name: string;
  url: string;
  username: string;
  password: string;
  priority: number;
  v2_authority_pubkey?: string;
}

export type DefaultPoolIndex = 0;
export type BackupPoolIndex = 1 | 2;
export type PoolIndex = DefaultPoolIndex | BackupPoolIndex;
