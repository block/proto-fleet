import { ReactNode, useState } from "react";
import clsx from "clsx";

import {
  NetworkInfoNetworkinfo,
  SystemInfoSysteminfo,
} from "@/protoOS/api/types";

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
  fullScreen?: boolean;
  networkInfo?: NetworkInfoNetworkinfo;
  onDismissLogin?: () => void;
  onSuccessLogin: () => void;
  pendingNetworkInfo: boolean;
  pendingSystemInfo: boolean;
  showLoginModal: boolean;
  systemInfo?: SystemInfoSysteminfo;
  title: string;
  type: NavigationMenuType;
}

const AppLayout = ({
  children,
  customButtons,
  fullScreen,
  networkInfo,
  onDismissLogin,
  onSuccessLogin,
  pendingNetworkInfo,
  pendingSystemInfo,
  showLoginModal,
  systemInfo,
  title,
  type,
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

      <div className="fixed right-4 bottom-4 z-10">
        <Toaster />
      </div>

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
            type={type}
          />
        </div>
        <div className="w-full">
          <PageHeader
            title={title}
            openMenu={() => setIsMenuOpen(true)}
            customButtons={customButtons}
          />
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
                {children}
              </div>
            </div>
          </div>
        </div>
      </div>
    </>
  );
};

export default AppLayout;
