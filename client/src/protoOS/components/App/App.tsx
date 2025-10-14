import { ComponentType, ReactNode, useCallback, useMemo } from "react";
import { useLocation } from "react-router-dom";

import ErrorCallout from "./ErrorCallout";
import WakeCallout from "./WakeCallout";
import WarmingUpCallout from "./WarmingUpCallout";
import { useNetworkInfo } from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { SystemInfoSysteminfo } from "@/protoOS/api/generatedApi";
import AppLayout from "@/protoOS/components/AppLayout";
import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { navigationMenuTypes } from "@/protoOS/components/NavigationMenu";
import { WarnWakeDialog } from "@/protoOS/components/Power";
import {
  useAccessToken,
  useAuthContext,
} from "@/protoOS/features/auth/contexts/AuthContext";
import {
  useComprehensiveStatus,
  useIsSleeping,
  useIsWarmingUp,
  useMinerErrors,
  useWakeDialog,
} from "@/protoOS/store";
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
      navigate(location.state?.from || "/");
    }
    setDismissedLoginModal(true);
  }, [navigate, pathname, setDismissedLoginModal, location]);

  // Use granular hooks to avoid unnecessary re-renders at the app level
  const isWarmingUp = useIsWarmingUp();
  const isSleeping = useIsSleeping();
  const errors = useMinerErrors();
  const comprehensiveStatus = useComprehensiveStatus();
  const wakeDialog = useWakeDialog();

  useAccessToken();

  return (
    <>
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
        {isWarmingUp ? (
          <WarmingUpCallout />
        ) : (
          <WakeCallout afterWake={afterWake} onWake={onWake} />
        )}
        {!isWarmingUp && !isSleeping && errors.errors?.length && !hideErrors ? (
          <ErrorCallout status={comprehensiveStatus} />
        ) : null}
        {children}
      </AppLayout>
      <WarnWakeDialog
        onClose={wakeDialog.onClose}
        onSubmit={wakeDialog.onConfirm}
        show={wakeDialog.show}
      />
    </>
  );
};

export default App;
