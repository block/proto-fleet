import { ComponentType, ReactNode, useState } from "react";

import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

import NavigationMenu, { NavigationMenuType } from "@/protoOS/components/NavigationMenu";

import PageHeader from "@/protoOS/components/PageHeader";
import {
  useIpAddress,
  useMacAddress,
  useNetworkInfoPending,
  useOSVersion,
  useProductName,
  useSystemInfoPending,
} from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

interface AppLayoutProps {
  children: ReactNode;
  customHeaderButtons?: ReactNode;
  title: string;
  type: NavigationMenuType;
  ContentLayout?: ComponentType<ContentLayoutProps>;
}

const AppLayout = ({
  children,
  customHeaderButtons,
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

  return (
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
        <PageHeader title={title} openMenu={() => setIsMenuOpen(true)} customButtons={customHeaderButtons} />
        <div className="relative w-full pt-[100px] pl-0 laptop:pt-[60px] laptop:pl-60">
          <ErrorBoundary>
            <ContentLayout>{children}</ContentLayout>
          </ErrorBoundary>
        </div>
      </div>
    </div>
  );
};

export default AppLayout;
