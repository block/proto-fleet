import React, { useEffect, useState } from "react";
import {
  Api,
  NetworkInfoNetworkinfo,
  HashboardsInfoHashboardsinfo,
} from "./Api";
import Navigation from "components/Navigation";

const { api } = new Api();

interface AppProps {
  children?: React.ReactNode;
}

const App = ({ children }: AppProps) => {
  const [networkInfo, setNetworkInfo] = useState({} as NetworkInfoNetworkinfo);
  const [hashboardsInfo, setHashboardsInfo] = useState<
    HashboardsInfoHashboardsinfo[]
  >([]);

  useEffect(() => {
    api.getNetwork().then((res) => {
      if (res.data["network-info"]) {
        setNetworkInfo(res.data["network-info"]);
      }
    });
    api.hashboards().then((res) => {
      if (res.data["hashboards-info"]) {
        setHashboardsInfo(res.data["hashboards-info"]);
      }
    });
  }, []);

  return (
    <div className="flex">
      <div className="grow">
        <Navigation
          hashboard_serials={hashboardsInfo.map((hashboard) => hashboard.hb_sn)}
          controller_ip={networkInfo.ip}
          controller_mac={networkInfo.mac}
        />
      </div>
      <div className="w-full p-6">{children}</div>
    </div>
  );
};

export default App;
