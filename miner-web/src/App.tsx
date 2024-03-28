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
    fetch: fetchPoolsInfo,
  } = usePoolsInfo();

  useEffect(() => {
    fetchPoolsInfo();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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
          <div className="w-full h-[calc(100%-56px)] overflow-scroll">
            <div className="m-20 flex justify-center">
              <div className="w-[880px]">{children}</div>
            </div>
          </div>
        </div>
      </div>
    </ApiContext.Provider>
  );
};

export default App;
