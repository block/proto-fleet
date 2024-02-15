import React, { useEffect, useState } from "react";

import { api, useHashboards, useNetworkInfo, usePoolsInfo } from "api";
import { Pool } from "apiTypes";

import Navigation from "components/Navigation";

interface AppProps {
  children?: React.ReactNode;
}

const App = ({ children }: AppProps) => {
  const [hashboardSerials, setHashboardSerials] = useState<string[]>();
  const [poolInfo, setPoolInfo] = useState<Pick<Pool, "status" | "url">>();

  const { data: networkInfo, pending: pendingNetworkInfo } = useNetworkInfo();
  const {
    data: poolsInfo,
    pending: pendingPoolsInfo,
    error: errorPoolsInfo,
  } = usePoolsInfo();
  const { data: hashboards, pending: pendingHashboards } = useHashboards();

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

  useEffect(() => {
    if (hashboards) {
      const serials = hashboards
        .map((hashboard) => hashboard.hb_sn)
        .filter((serial) => serial !== undefined) as string[];
      setHashboardSerials(serials);
    }
  }, [hashboards]);

  return (
    <div className="flex max-w-[1440px] h-screen">
      <div className="grow">
        <Navigation
          hashboard_serials={{
            value: hashboardSerials,
            loading: pendingHashboards,
          }}
          controller_ip={{
            value: networkInfo?.ip,
            loading: pendingNetworkInfo,
          }}
          controller_mac={{
            value: networkInfo?.mac,
            loading: pendingNetworkInfo,
          }}
          pool_info={{
            status: poolInfo?.status,
            url: poolInfo?.url,
            loading: pendingPoolsInfo,
            error: errorPoolsInfo,
          }}
          onClickReboot={api.rebootSystem}
          onClickSleep={api.stopMining}
        />
      </div>
      <div className="w-full m-14">{children}</div>
    </div>
  );
};

export default App;
