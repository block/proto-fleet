import React, { useEffect, useState } from "react";

import { Api, NetworkInfoNetworkinfo, Pool } from "Api";

import {
  getSerialNumbersDisplay,
  getUrlDisplay,
} from "common/utils/stringUtils";

import Navigation from "components/Navigation";

const { api } = new Api();

interface AppProps {
  children?: React.ReactNode;
}

const App = ({ children }: AppProps) => {
  const [networkInfo, setNetworkInfo] = useState({} as NetworkInfoNetworkinfo);
  const [hashboardSerials, setHashboardSerials] = useState<
    (string | undefined)[] | undefined
  >([]);
  const [poolInfo, setPoolInfo] = useState([] as Pool | undefined);

  useEffect(() => {
    api.getNetwork().then((res) => {
      if (res.data["network-info"]) {
        setNetworkInfo(res.data["network-info"]);
      }
    });
    api.hashboards().then((res) => {
      if (res.data["hashboards-info"]) {
        setHashboardSerials(
          getSerialNumbersDisplay(
            res.data["hashboards-info"]?.map((hashboard) => hashboard.hb_sn)
          )
        );
      }
    });
    api.listPools().then((res) => {
      if (res.data["pools"]) {
        setPoolInfo(res.data["pools"].find((pool) => pool.priority === 0));
      }
    });
  }, []);

  return (
    <div className="flex">
      <div className="grow">
        <Navigation
          hashboard_serials={hashboardSerials}
          controller_ip={networkInfo.ip}
          controller_mac={networkInfo.mac?.replace(/\./g, ":")}
          pool_info={{
            status: poolInfo?.status,
            url: getUrlDisplay(poolInfo?.url),
          }}
        />
      </div>
      <div className="w-full p-6">{children}</div>
    </div>
  );
};

export default App;
