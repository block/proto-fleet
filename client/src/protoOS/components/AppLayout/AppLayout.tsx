import { ComponentType, ReactNode, useState } from "react";

import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

import NavigationMenu, { NavigationMenuType } from "@/protoOS/components/NavigationMenu";

import PageHeader from "@/protoOS/components/PageHeader";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
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
  const { metadata = {}, isFleetHosted } = useMinerHosting();

  // Read system info from store
  const osVersion = useOSVersion();
  const productName = useProductName();
  const pendingSystemInfo = useSystemInfoPending();

  // Read network info from store
  const macAddress = useMacAddress();
  const ipAddress = useIpAddress();
  const pendingNetworkInfo = useNetworkInfoPending();

  // Placement is a Fleet-only concept and rides in on the hosting metadata.
  // Show a single "Location" row that stacks site / building / rack on their
  // own lines, omitting any levels the miner isn't placed at.
  const locationLines = [metadata.site, metadata.building, metadata.rack].filter(Boolean) as string[];
  const locationInfo = isFleetHosted && locationLines.length ? { values: locationLines } : undefined;

  return (
    <div className="flex min-h-screen bg-surface-base">
      <div className="fixed top-0 left-0 z-40 h-screen grow overflow-hidden">
        <NavigationMenu
          macInfo={{
            // Fleet snapshots often omit mac/ip/firmware; fall back to the
            // device's own values, which the proxy fetches into the store.
            value: isFleetHosted ? metadata.macAddress || macAddress : macAddress,
            loading: isFleetHosted ? !metadata.macAddress && pendingNetworkInfo : pendingNetworkInfo,
          }}
          isVisible={isMenuOpen}
          closeMenu={() => setIsMenuOpen(false)}
          locationInfo={locationInfo}
          versionInfo={{
            value: isFleetHosted ? metadata.firmwareVersion || osVersion : osVersion,
            loading: isFleetHosted ? !metadata.firmwareVersion && pendingSystemInfo : pendingSystemInfo,
          }}
          ipAddressInfo={{
            value: isFleetHosted ? metadata.ipAddress || ipAddress : ipAddress,
            loading: isFleetHosted ? !metadata.ipAddress && pendingNetworkInfo : pendingNetworkInfo,
          }}
          minerNameInfo={{
            value: isFleetHosted ? metadata.minerName : productName,
            loading: isFleetHosted ? false : pendingSystemInfo,
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
