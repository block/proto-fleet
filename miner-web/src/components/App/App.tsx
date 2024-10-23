import { ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import { useLocation } from "react-router-dom";

import { ApiContext, useNetworkInfo, usePoll, usePoolsInfo } from "api";
import { ErrorProps } from "apiResponseTypes";
import {
  ErrorListResponse,
  MiningStatusMiningstatus,
  SystemInfoSysteminfo,
} from "apiTypes";

import { useAuthContext } from "common/hooks/useAuthContext";
import { useNavigate } from "common/hooks/useNavigate";

import AppLayout from "components/AppLayout";
import { navigationMenuTypes } from "components/NavigationMenu";

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
  pendingSystemInfo: boolean;
  systemInfo?: SystemInfoSysteminfo;
  title: string;
  wakeError?: ErrorProps;
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
  wakeError,
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
  const { showLoginModal, setShowLoginModal, setDismissedLoginModal } =
    useAuthContext();
  const navigate = useNavigate();
  const location = useLocation();
  const { pathname } = useMemo(() => location, [location]);

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

  const handleDismissLogin = useCallback(() => {
    if (pathname === "/settings/mining-pools") {
      // if user landed on mining pools page from within the app, navigate back
      // else navigate to home
      navigate(location.state?.from || "/home");
    }
    setShowLoginModal(false);
    setDismissedLoginModal(true);
  }, [navigate, pathname, setDismissedLoginModal, setShowLoginModal, location]);

  return (
    <ApiContext.Provider
      value={{
        errors: {
          errors: errors || [],
          pending: !!(pendingErrors && !errors),
        },
        miningStatus: miningStatus || {},
        setMiningStatus,
        poolsInfo,
        fetchPoolsInfo,
        poolsInfoStatus: {
          error: errorPoolsInfo || "",
          pending: pendingPoolsInfo && !poolsInfo,
        },
      }}
    >
      <AppLayout
        fullScreen={fullScreen}
        networkInfo={networkInfo}
        onContinueLogin={() => setShowLoginModal(false)}
        onDismissLogin={handleDismissLogin}
        pendingNetworkInfo={pendingNetworkInfo}
        pendingSystemInfo={pendingSystemInfo}
        showLoginModal={showLoginModal}
        systemInfo={systemInfo}
        title={title}
        type={navigationMenuTypes.app}
      >
        {isWarmingUp(miningStatus) ? (
          <WarmingUpCallout />
        ) : (
          <WakeCallout
            afterWake={afterWake}
            miningStatus={miningStatus}
            onWake={onWake}
            wakeError={wakeError}
          />
        )}
        {!isWarmingUp(miningStatus) &&
        !isSleeping(miningStatus?.status) &&
        errors?.length &&
        !hideErrors ? (
          <ErrorCallout errors={errors} />
        ) : null}
        {children}
      </AppLayout>
    </ApiContext.Provider>
  );
};

export default App;
