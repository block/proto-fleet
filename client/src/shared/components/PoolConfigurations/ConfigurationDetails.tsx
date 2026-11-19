import type { ReactNode } from "react";
import { poolSlotLabel } from "./types";

interface PoolDetails {
  url: string;
  username: string;
  hasPassword?: boolean;
}

export function ConfigurationPools({ pools }: { pools: ReadonlyArray<PoolDetails> }) {
  return (
    <div className="flex flex-col gap-6" data-dd-privacy="mask">
      {pools.map((pool, index) => (
        <div key={index} className="flex flex-col gap-2 break-all">
          <h3 className="text-heading-100">{poolSlotLabel(index)}</h3>
          <p>{pool.url}</p>
          <p className="whitespace-pre-wrap">Username: {pool.username}</p>
          {pool.hasPassword !== undefined ? <p>{pool.hasPassword ? "Password saved" : "No password"}</p> : null}
        </div>
      ))}
    </div>
  );
}
export function ConfigurationDetails({
  assignedCount,
  pools,
  children,
}: {
  assignedCount: number | bigint;
  pools: ReadonlyArray<PoolDetails>;
  children?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-6" data-dd-privacy="mask">
      <p>{assignedCount.toString()} equipment assigned</p>
      {children}
      <ConfigurationPools pools={pools} />
    </div>
  );
}
