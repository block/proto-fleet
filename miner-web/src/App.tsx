import React, { useEffect, useState } from "react";

import { useNetworkInfo, usePoolsInfo } from "api";
import { Pool } from "apiTypes";

import Navigation from "components/Navigation";
import PageHeader from "components/PageHeader";

interface AppProps {
  children?: React.ReactNode;
  title: string;
}

const App = ({ children, title }: AppProps) => {
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
    <div className="flex h-screen bg-core-primary-fill">
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
        <PageHeader title={title} />
        <div className="m-20 max-w-[880px]">{children}</div>
      </div>
    </div>
  );
};

export default App;
