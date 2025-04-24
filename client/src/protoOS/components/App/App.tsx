import {
  ComponentType,
  ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { useLocation } from "react-router-dom";

import ErrorCallout from "./ErrorCallout";
import { isSleeping, isWarmingUp } from "./utility";
import WakeCallout from "./WakeCallout";
import WarmingUpCallout from "./WarmingUpCallout";
import { useNetworkInfo, usePoll, usePoolsInfo } from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import {
  ErrorListResponse,
  MiningStatusMiningstatus,
  SystemInfoSysteminfo,
} from "@/protoOS/api/types";
import AppLayout from "@/protoOS/components/AppLayout";
import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { navigationMenuTypes } from "@/protoOS/components/NavigationMenu";
import { useAuthContext } from "@/protoOS/contexts/AuthContext";
import { MinerStatusContext } from "@/protoOS/contexts/MinerStatusContext";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface AppProps {
  afterWake?: () => void;
  apiErrors?: ErrorListResponse;
  apiMiningStatus?: MiningStatusMiningstatus;
  children?: ReactNode;
  hideErrors?: boolean;
  onWake?: () => void;
  pendingErrors?: boolean;
  pendingSystemInfo: boolean;
  systemInfo?: SystemInfoSysteminfo;
  title: string;
  wakeError?: ErrorProps;
  ContentLayout?: ComponentType<ContentLayoutProps>;
}

const App = ({
  afterWake,
  apiErrors,
  apiMiningStatus,
  children,
  hideErrors,
  onWake,
  pendingErrors,
  pendingSystemInfo,
  systemInfo,
  title,
  wakeError,
  ContentLayout = DefaultContentLayout,
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
    <MinerStatusContext.Provider
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
        networkInfo={networkInfo}
        onSuccessLogin={() => setShowLoginModal(false)}
        onDismissLogin={handleDismissLogin}
        pendingNetworkInfo={pendingNetworkInfo}
        pendingSystemInfo={pendingSystemInfo}
        showLoginModal={showLoginModal}
        systemInfo={systemInfo}
        title={title}
        type={navigationMenuTypes.app}
        ContentLayout={ContentLayout}
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
    </MinerStatusContext.Provider>
  );
};

export default App;
