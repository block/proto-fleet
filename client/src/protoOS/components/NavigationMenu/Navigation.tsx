import { useCallback, useMemo } from "react";
import { Link, useLocation } from "react-router-dom";
import clsx from "clsx";

import { navigationItems, navigationMenuTypes } from "./constants";
import InfoItem from "./InfoItem";
import IpAddressInfo from "./InfoItem/IpAddressInfo";
import type { IpAddressInfoProps } from "./InfoItem/IpAddressInfo";
import LocationInfo from "./InfoItem/LocationInfo";
import type { LocationInfoProps } from "./InfoItem/LocationInfo";
import MacAddressInfo from "./InfoItem/MacAddressInfo";
import type { MacAddressInfoProps } from "./InfoItem/MacAddressInfo";
import MinerNameInfo from "./InfoItem/MinerNameInfo";
import type { MinerNameInfoProps } from "./InfoItem/MinerNameInfo";
import VersionInfo from "./InfoItem/VersionInfo";
import type { VersionInfoProps } from "./InfoItem/VersionInfo";
import { AppNavigationItems, OnboardingNavigationItems } from "./NavigationItems";
import { NavigationItemValue, NavigationMenuType } from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useDeviceType } from "@/protoOS/store";
import { Logo } from "@/shared/assets/icons";
import { useNavigate } from "@/shared/hooks/useNavigate";
import { buildVersionInfo } from "@/shared/utils/version";

interface NavigationProps {
  ipAddressInfo?: IpAddressInfoProps;
  locationInfo?: LocationInfoProps;
  macInfo?: MacAddressInfoProps;
  minerNameInfo?: MinerNameInfoProps;
  onItemClick?: () => void;
  versionInfo?: VersionInfoProps;
  type: NavigationMenuType;
}

const Navigation = ({
  ipAddressInfo,
  locationInfo,
  macInfo,
  minerNameInfo,
  onItemClick,
  versionInfo,
  type,
}: NavigationProps) => {
  const isApp = useMemo(() => type === navigationMenuTypes.app, [type]);

  const { minerRoot, closeButton } = useMinerHosting();
  const deviceType = useDeviceType();
  const appHomeItem = deviceType === "container" ? navigationItems.containerHome : navigationItems.home;

  const isOnboarding = useMemo(() => type === navigationMenuTypes.onboarding, [type]);

  const navigate = useNavigate();
  const location = useLocation();
  const { pathname } = useMemo(() => location, [location]);
  const pageName = useMemo(() => {
    const localPath =
      minerRoot && (pathname === minerRoot || pathname.startsWith(`${minerRoot}/`))
        ? pathname.slice(minerRoot.length)
        : pathname;
    const route = localPath.replace(/^\//, "").replace(/\/$/, "");
    if (route.length) {
      return route;
    } else {
      return isApp ? appHomeItem : navigationItems.onboarding;
    }
  }, [pathname, minerRoot, isApp, appHomeItem]);

  const handleClick = useCallback(
    (navigationItem: NavigationItemValue) => {
      navigate(`${minerRoot}/${navigationItem}`);
      onItemClick?.();
    },
    [onItemClick, navigate, minerRoot],
  );

  return (
    <div
      className={clsx(
        "absolute z-30 flex h-full max-h-[calc(100vh-16px)] w-60 flex-col overflow-auto rounded-lg border-r border-border-5 bg-surface-base text-text-primary-70",
        "laptop:static laptop:z-auto laptop:max-h-screen laptop:rounded-none",
      )}
    >
      <div className="grow">
        <div className="mb-3 flex h-[60px] items-center px-3 py-2">
          {closeButton ? (
            closeButton
          ) : (
            <Link to={isApp ? `${minerRoot}/${appHomeItem}` : `${minerRoot}/${navigationItems.onboarding}`}>
              <Logo className="text-text-primary hover:cursor-pointer" />
            </Link>
          )}
        </div>
        <div className="px-3" data-testid="navigation">
          {isApp ? <AppNavigationItems pageName={pageName} onClick={handleClick} /> : null}
          {isOnboarding ? <OnboardingNavigationItems pageName={pageName} onClick={handleClick} /> : null}
        </div>
      </div>

      <div className="px-3 pb-3">
        <MinerNameInfo loading={minerNameInfo?.loading} value={minerNameInfo?.value} />

        {locationInfo ? <LocationInfo loading={locationInfo.loading} values={locationInfo.values} /> : null}

        <IpAddressInfo loading={ipAddressInfo?.loading} value={ipAddressInfo?.value} />

        <VersionInfo loading={versionInfo?.loading} value={versionInfo?.value} />

        {/* The web client's own build, which ships separately from the device firmware above. */}
        <InfoItem
          label="ProtoOS Version"
          value={buildVersionInfo.version}
          divider={false}
          testId="protoos-version-info-item"
        />

        <MacAddressInfo loading={macInfo?.loading} value={macInfo?.value} />
      </div>
    </div>
  );
};

export default Navigation;
