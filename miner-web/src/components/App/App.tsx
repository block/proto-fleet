import { ReactNode, useEffect, useState } from "react";
import clsx from "clsx";

import { ApiContext, useNetworkInfo, usePoll, usePoolsInfo } from "api";
import {
  ErrorListResponse,
  MiningStatusMiningstatus,
  SystemInfoSysteminfo,
} from "apiTypes";

import NavigationMenu from "components/NavigationMenu";
import PageHeader from "components/PageHeader";

import ErrorCallout from "./ErrorCallout";
import { isSleeping, isWarmingUp } from "./utility";
import WakeCallout from "./WakeCallout";
import WarmingUpCallout from "./WarmingUpCallout";

interface AppProps {
  afterWake?: () => void;
  apiErrors?: ErrorListResponse;
  apiMiningStatus?: MiningStatusMiningstatus;
  children?: ReactNode;
  fullScreen?: boolean;
  hideErrors?: boolean;
  onWake?: () => void;
  pendingErrors?: boolean;
  pendingSystemInfo?: boolean;
  systemInfo?: SystemInfoSysteminfo;
  title: string;
}

const App = ({
  afterWake,
  apiErrors,
  apiMiningStatus,
  children,
  fullScreen,
  hideErrors,
  onWake,
  pendingErrors,
  pendingSystemInfo,
  systemInfo,
  title,
}: AppProps) => {
  const { data: networkInfo, pending: pendingNetworkInfo } = useNetworkInfo();
  const {
    data: poolsInfo,
    error: errorPoolsInfo,
    fetchData: fetchPoolsInfo,
    pending: pendingPoolsInfo,
  } = usePoolsInfo();
  const [miningStatus, setMiningStatus] = useState<
    MiningStatusMiningstatus | undefined
  >(apiMiningStatus);
  const [errors, setErrors] = useState(apiErrors);
  const [isMenuOpen, setIsMenuOpen] = useState(false);

  useEffect(() => {
    if (apiMiningStatus !== undefined) {
      setMiningStatus(apiMiningStatus);
    }
  }, [apiMiningStatus]);

  useEffect(() => {
    if (apiErrors !== undefined) {
      setErrors(apiErrors);
    }
  }, [apiErrors]);

  usePoll({
    fetchData: () => fetchPoolsInfo({ retryOnMinerDown: true }),
    poll: true,
  });

  return (
    <ApiContext.Provider
      value={{
        errors: { errors: errors || [], pending: !!pendingErrors },
        miningStatus: miningStatus || {},
        setMiningStatus,
        poolsInfo: poolsInfo,
        fetchPoolsInfo,
        poolsInfoStatus: {
          error: errorPoolsInfo || "",
          pending: pendingPoolsInfo,
        },
      }}
    >
      <div className="flex h-screen bg-surface-base">
        <div className="grow">
          <NavigationMenu
            macInfo={{
              value: networkInfo?.mac,
              loading: pendingNetworkInfo,
            }}
            isVisible={isMenuOpen}
            closeMenu={() => setIsMenuOpen(false)}
            versionInfo={{
              value: systemInfo?.os?.version,
              loading: pendingSystemInfo,
            }}
          />
        </div>
        <div className="w-full">
          <PageHeader title={title} openMenu={() => setIsMenuOpen(true)} />
          <div className="w-full h-[calc(100%-60px)] overflow-y-scroll relative">
            <div
              className={clsx("min-h-[calc(100%-60px-60px)]", {
                "flex justify-center m-14 tablet:m-6 phone:m-6": !fullScreen,
              })}
            >
              <div
                className={clsx({
                  "desktop:w-[928px] laptop:w-[608px] tablet:w-[584px] phone:w-[352px]":
                    !fullScreen,
                })}
              >
                {isWarmingUp(miningStatus) ? (
                  <WarmingUpCallout />
                ) : (
                  <WakeCallout
                    afterWake={afterWake}
                    miningStatus={miningStatus}
                    onWake={onWake}
                  />
                )}
                {!isWarmingUp(miningStatus) && !isSleeping(miningStatus?.status) && errors?.length && !hideErrors ? (
                  <ErrorCallout errors={errors} />
                ) : null}
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
