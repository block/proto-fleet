import { ComponentType, ReactNode, useState } from "react";

import {
  NetworkInfoNetworkinfo,
  SystemInfoSysteminfo,
} from "@/protoOS/api/types";

import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import LoginModal from "@/protoOS/components/LoginModal";

import NavigationMenu, {
  NavigationMenuType,
} from "@/protoOS/components/NavigationMenu";

import PageHeader from "@/protoOS/components/PageHeader";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
  Toaster,
} from "@/shared/features/toaster";

interface AppLayoutProps {
  children: ReactNode;
  customButtons?: ReactNode;
  networkInfo?: NetworkInfoNetworkinfo;
  onDismissLogin?: () => void;
  onSuccessLogin: () => void;
  pendingNetworkInfo: boolean;
  pendingSystemInfo: boolean;
  showLoginModal: boolean;
  systemInfo?: SystemInfoSysteminfo;
  title: string;
  type: NavigationMenuType;
  ContentLayout?: ComponentType<ContentLayoutProps>;
}

const AppLayout = ({
  children,
  customButtons,
  networkInfo,
  onDismissLogin,
  onSuccessLogin,
  pendingNetworkInfo,
  pendingSystemInfo,
  showLoginModal,
  systemInfo,
  title,
  type,
  ContentLayout = DefaultContentLayout,
}: AppLayoutProps) => {
  const [isMenuOpen, setIsMenuOpen] = useState(false);

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
              value: networkInfo?.mac,
              loading: pendingNetworkInfo,
            }}
            isVisible={isMenuOpen}
            closeMenu={() => setIsMenuOpen(false)}
            versionInfo={{
              value: systemInfo?.os?.version,
              loading: pendingSystemInfo,
            }}
            type={type}
          />
        </div>
        <div className="w-full">
          <PageHeader
            title={title}
            openMenu={() => setIsMenuOpen(true)}
            customButtons={customButtons}
          />
          <div className="relative w-full pt-[60px] pl-60 phone:pl-0 tablet:pl-0">
            <ContentLayout>{children}</ContentLayout>
          </div>
        </div>
      </div>
    </>
  );
};

export default AppLayout;
