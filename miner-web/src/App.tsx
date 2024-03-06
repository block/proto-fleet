import React, { useEffect, useState } from "react";

import { useNetworkInfo, usePoolsInfo } from "api";
import { Pool } from "apiTypes";

import Navigation from "components/Navigation";

interface AppProps {
  children?: React.ReactNode;
}

const App = ({ children }: AppProps) => {
  const [poolInfo, setPoolInfo] = useState<Pick<Pool, "status" | "url">>();

  const { data: networkInfo, pending: pendingNetworkInfo } = useNetworkInfo();
  const {
    data: poolsInfo,
    pending: pendingPoolsInfo,
    error: errorPoolsInfo,
    fetch: fetchPoolsInfo,
  } = usePoolsInfo();

  useEffect(() => {
    if (!poolsInfo && !pendingPoolsInfo && !errorPoolsInfo) {
      fetchPoolsInfo();
    }
  }, [errorPoolsInfo, fetchPoolsInfo, pendingPoolsInfo, poolsInfo]);

  useEffect(() => {
    if (poolsInfo) {
      const activePool =
        poolsInfo.find((pool) => pool.status === "Alive") || poolsInfo[0];
      setPoolInfo({
        status: activePool?.status,
        url: activePool?.url,
      });
    }
  }, [poolsInfo]);

  return (
    <div className="flex max-w-[1440px] h-screen bg-core-primary-fill">
      <div className="grow">
        <Navigation
          macInfo={{
            value: networkInfo?.mac,
            loading: pendingNetworkInfo,
          }}
          poolInfo={{
            status: poolInfo?.status,
            url: poolInfo?.url,
            loading: pendingPoolsInfo,
            error: !!errorPoolsInfo,
          }}
        />
      </div>
      <div className="w-full rounded-s-2xl bg-surface-base">
        <div className="m-14">{children}</div>
      </div>
    </div>
  );
};

export default App;
