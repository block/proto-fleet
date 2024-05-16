import { ReactNode, useState } from "react";

import { useNetworkInfo, usePoolsInfo } from "api";

import NavigationMenu from "components/NavigationMenu";
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
  } = usePoolsInfo(true);
  const [isMenuOpen, setIsMenuOpen] = useState(false);

  return (
    <ApiContext.Provider
      value={{
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
          <div className="w-full h-[calc(100%-56px)] overflow-y-scroll">
            <div className="m-14 tablet:m-6 phone:m-6 flex justify-center">
              <div className="desktop:w-[928px] laptop:w-[608px] tablet:w-[584px] phone:w-[352px]">
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
