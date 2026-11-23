import type { ReactNode } from "react";
import { poolSlotLabel } from "./types";
export function ConfigurationDetails({
  equipment,
  pools,
  children,
}: {
  equipment: ReactNode;
  pools: ReadonlyArray<{ url: string; username: string; hasPassword?: boolean }>;
  children?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-6" data-dd-privacy="mask">
      {equipment}
      {children}
      {pools.map((pool, index) => (
        <div key={index} className="flex flex-col gap-2 break-all">
          <h3 className="text-heading-100">{poolSlotLabel(index)}</h3>
          <p>{pool.url}</p>
          <p>Username: {pool.username}</p>
          {pool.hasPassword !== undefined ? <p>{pool.hasPassword ? "Password saved" : "No password"}</p> : null}
        </div>
      ))}
    </div>
  );
}
