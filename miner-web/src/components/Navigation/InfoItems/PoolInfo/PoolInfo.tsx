import { useMemo } from "react";

import { Pool } from "apiTypes";

import { getPoolUrlDisplay } from "common/utils/stringUtils";

import InfoItem from "../InfoItem";

export interface PoolProps {
  error?: boolean;
  loading?: boolean;
  status?: Pool["status"];
  url?: Pool["url"];
}

const PoolInfo = ({ error, loading, status, url }: PoolProps) => {
  const isPoolConnected = useMemo(() => status === "Alive", [status]);
  const displayUrl = useMemo(() => getPoolUrlDisplay(url), [url]);

  return (
    <InfoItem
      label={`Pool Connection ${loading || isPoolConnected ? "" : "(failed)"}`}
      value={displayUrl}
      badge={loading ? undefined : isPoolConnected ? "success" : "error"}
      loading={loading}
      error={error}
    />
  );
};

export default PoolInfo;
