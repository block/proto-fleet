import { ReactNode, useEffect, useState } from "react";

import { ApiContext, useNetworkInfo, usePoolsInfo } from "api";
import { MiningStatusMiningstatus } from "apiTypes";

import NavigationMenu from "components/NavigationMenu";
import PageHeader from "components/PageHeader";

import WakeCallout from "./WakeCallout";

interface AppProps {
  afterWake?: () => void;
  apiMiningStatus?: MiningStatusMiningstatus;
  children?: ReactNode;
  onWake: () => void;
  title: string;
}

const App = ({
  afterWake,
  apiMiningStatus,
  children,
  onWake,
  title,
}: AppProps) => {
  const { data: networkInfo, pending: pendingNetworkInfo } = useNetworkInfo();
  const { data: poolsInfo, pending: pendingPoolsInfo } = usePoolsInfo(true);
  const [miningStatus, setMiningStatus] = useState<
    MiningStatusMiningstatus | undefined
  >(apiMiningStatus);
  const [isMenuOpen, setIsMenuOpen] = useState(false);

  useEffect(() => {
    if (apiMiningStatus !== undefined) {
      setMiningStatus(apiMiningStatus);
    }
  }, [apiMiningStatus]);

  return (
    <ApiContext.Provider
      value={{
        miningStatus: miningStatus || {},
        setMiningStatus,
        poolsInfo: poolsInfo || [],
        poolsInfoStatus: { pending: pendingPoolsInfo },
      }}
    >
      <div className="flex h-screen bg-core-primary-fill">
        <div className="grow">
          <NavigationMenu
            macInfo={{
              value: networkInfo?.mac,
              loading: pendingNetworkInfo,
            }}
            isVisible={isMenuOpen}
            closeMenu={() => setIsMenuOpen(false)}
          />
        </div>
        <div className="w-full laptop:rounded-s-2xl desktop:laptop:rounded-s-2xl bg-surface-base">
          <PageHeader title={title} openMenu={() => setIsMenuOpen(true)} />
          <div className="w-full h-[calc(100%-56px)] overflow-y-scroll relative">
            <div className="h-full m-14 tablet:m-6 phone:m-6 flex justify-center">
              <div className="desktop:w-[928px] laptop:w-[608px] tablet:w-[584px] phone:w-[352px]">
                <WakeCallout afterWake={afterWake} miningStatus={miningStatus} onWake={onWake} />
                {children}
              </div>
            </div>
          </div>
        </div>
      </div>
    </ApiContext.Provider>
  );
};

export default App;
