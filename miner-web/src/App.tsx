import { ReactNode, useEffect } from "react";

import { useNetworkInfo, usePoolsInfo } from "api";

import Navigation from "components/Navigation";
import PageHeader from "components/PageHeader";
import { ApiContext } from "./api/api";

interface AppProps {
  children?: ReactNode;
  title: string;
}

const App = ({ children, title }: AppProps) => {
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

  return (
    <ApiContext.Provider
      value={{
        poolsInfo: poolsInfo || [],
        poolsInfoStatus: { pending: pendingPoolsInfo },
      }}
    >
      <div className="flex h-screen bg-core-primary-fill">
        <div className="grow">
          <Navigation
            macInfo={{
              value: networkInfo?.mac,
              loading: pendingNetworkInfo,
            }}
          />
        </div>
        <div className="w-full rounded-s-2xl bg-surface-base">
          <PageHeader title={title} />
          <div className="m-20 max-w-[880px]">{children}</div>
        </div>
      </div>
    </ApiContext.Provider>
  );
};

export default App;
