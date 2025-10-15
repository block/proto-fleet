import { ComponentType, ReactNode, useState } from "react";

import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

import NavigationMenu, {
  NavigationMenuType,
} from "@/protoOS/components/NavigationMenu";

import PageHeader from "@/protoOS/components/PageHeader";
import LoginModal from "@/protoOS/features/auth/components/LoginModal";
import {
  useIpAddress,
  useMacAddress,
  useNetworkInfoPending,
  useOSVersion,
  useProductName,
  useSystemInfoPending,
} from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
  Toaster,
} from "@/shared/features/toaster";

interface AppLayoutProps {
  children: ReactNode;
  customHeaderButtons?: ReactNode;
  onDismissLogin?: () => void;
  onSuccessLogin: () => void;
  showLoginModal: boolean;
  title: string;
  type: NavigationMenuType;
  ContentLayout?: ComponentType<ContentLayoutProps>;
}

const AppLayout = ({
  children,
  customHeaderButtons,
  onDismissLogin,
  onSuccessLogin,
  showLoginModal,
  title,
  type,
  ContentLayout = DefaultContentLayout,
}: AppLayoutProps) => {
  const [isMenuOpen, setIsMenuOpen] = useState(false);

  // Read system info from store
  const osVersion = useOSVersion();
  const productName = useProductName();
  const pendingSystemInfo = useSystemInfoPending();

  // Read network info from store
  const macAddress = useMacAddress();
  const ipAddress = useIpAddress();
  const pendingNetworkInfo = useNetworkInfoPending();

  const handleOnSuccessLogin = () => {
    onSuccessLogin();
    pushToast({
      message: "You are now logged in as admin",
      status: TOAST_STATUSES.success,
    });
  };

  return (
    <>
      {showLoginModal && (
        <LoginModal
          onDismiss={onDismissLogin}
          onSuccess={handleOnSuccessLogin}
        />
      )}

      <div className="fixed right-4 bottom-4 z-10 phone:right-2 phone:bottom-2">
        <Toaster />
      </div>

      <div className="flex min-h-screen bg-surface-base">
        <div className="fixed top-0 left-0 z-40 h-screen grow overflow-hidden">
          <NavigationMenu
            macInfo={{
              value: macAddress,
              loading: pendingNetworkInfo,
            }}
            isVisible={isMenuOpen}
            closeMenu={() => setIsMenuOpen(false)}
            versionInfo={{
              value: osVersion,
              loading: pendingSystemInfo,
            }}
            ipAddressInfo={{
              value: ipAddress,
              loading: pendingNetworkInfo,
            }}
            minerNameInfo={{
              value: productName,
              loading: pendingSystemInfo,
            }}
            type={type}
          />
        </div>
        <div className="w-full">
          <PageHeader
            title={title}
            openMenu={() => setIsMenuOpen(true)}
            customButtons={customHeaderButtons}
          />
          <div className="relative w-full pt-[60px] pl-60 phone:pt-[100px] phone:pl-0 tablet:pt-[100px] tablet:pl-0">
            <ErrorBoundary>
              <ContentLayout>{children}</ContentLayout>
            </ErrorBoundary>
          </div>
        </div>
      </div>
    </>
  );
};

export default AppLayout;
