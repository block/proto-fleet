import { ComponentType, ReactNode, useCallback, useMemo } from "react";
import { useLocation } from "react-router-dom";

import ErrorCallout from "./ErrorCallout";
import { isSleeping, isWarmingUp } from "./utility";
import WakeCallout from "./WakeCallout";
import WarmingUpCallout from "./WarmingUpCallout";
import { useNetworkInfo } from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { SystemInfoSysteminfo } from "@/protoOS/api/types";
import AppLayout from "@/protoOS/components/AppLayout";
import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { navigationMenuTypes } from "@/protoOS/components/NavigationMenu";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import { useAuthContext } from "@/protoOS/features/auth/contexts/AuthContext";
import {
  InstallingOverlay,
  useFirmwareUpdate,
} from "@/protoOS/features/firmwareUpdate/";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface AppProps {
  afterWake?: () => void;
  children?: ReactNode;
  hideErrors?: boolean;
  onWake?: () => void;
  pendingSystemInfo: boolean;
  systemInfo?: SystemInfoSysteminfo;
  title: string;
  wakeError?: ErrorProps;
  ContentLayout?: ComponentType<ContentLayoutProps>;
}

const App = ({
  afterWake,
  children,
  hideErrors,
  onWake,
  pendingSystemInfo,
  systemInfo,
  title,
  wakeError,
  ContentLayout = DefaultContentLayout,
}: AppProps) => {
  const { data: networkInfo, pending: pendingNetworkInfo } = useNetworkInfo();
  const { showLoginModal, setShowLoginModal, setDismissedLoginModal } =
    useAuthContext();
  const navigate = useNavigate();
  const location = useLocation();
  const { pathname } = useMemo(() => location, [location]);
  const handleDismissLogin = useCallback(() => {
    if (pathname === "/settings/mining-pools") {
      // if user landed on mining pools page from within the app, navigate back
      // else navigate to home
      navigate(location.state?.from || "/home");
    }
    setShowLoginModal(false);
    setDismissedLoginModal(true);
  }, [navigate, pathname, setDismissedLoginModal, setShowLoginModal, location]);

  const { miningStatus, errors } = useMinerStatus();
  const { installing } = useFirmwareUpdate();

  return (
    <>
      {installing ? (
        <InstallingOverlay />
      ) : (
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
          errors.errors?.length &&
          !hideErrors ? (
            <ErrorCallout errors={errors.errors} />
          ) : null}
          {children}
        </AppLayout>
      )}
    </>
  );
};

export default App;
