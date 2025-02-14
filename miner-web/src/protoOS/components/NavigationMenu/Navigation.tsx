import { useCallback, useMemo, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import clsx from "clsx";

import { navigationItems, navigationMenuTypes } from "./constants";
import MacAddressInfo, { MacAddressInfoProps } from "./InfoItem/MacAddressInfo";
import VersionInfo, { VersionInfoProps } from "./InfoItem/VersionInfo";
import {
  AppNavigationItems,
  OnboardingNavigationItems,
} from "./NavigationItems";
import { NavigationItemValue, NavigationMenuType } from "./types";
import { useMinerHosting } from "@/protoOS/api";
import { Logo, ThemeLight } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import ThemeSwitcher from "@/shared/features/themes/ThemeSwitcher";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface NavigationProps {
  macInfo?: MacAddressInfoProps;
  onItemClick?: () => void;
  versionInfo?: VersionInfoProps;
  type: NavigationMenuType;
}

const Navigation = ({
  macInfo,
  onItemClick,
  versionInfo,
  type,
}: NavigationProps) => {
  const isApp = useMemo(() => type === navigationMenuTypes.app, [type]);

  const { minerRoot, closeButton } = useMinerHosting();

  const isOnboarding = useMemo(
    () => type === navigationMenuTypes.onboarding,
    [type]
  );

  const navigate = useNavigate();
  const location = useLocation();
  const { pathname } = useMemo(() => location, [location]);
  const pageName = useMemo(() => {
    // Remove leading slash
    const route = pathname.replace(/^\//, "");
    if (route.length) {
      return route;
    } else {
      return isApp ? navigationItems.home : navigationItems.onboarding;
    }
  }, [pathname, isApp]);

  const [showThemeSwitcher, setShowThemeSwitcher] = useState(false);

  const handleClick = useCallback(
    (navigationItem: NavigationItemValue) => {
      navigate(`${minerRoot}/${navigationItem}`);
      onItemClick?.();
    },
    [onItemClick, navigate, minerRoot]
  );

  return (
    <div
      className={clsx(
        "w-[240px] min-h-screen flex flex-col bg-surface-base text-text-primary-70 border-r border-border-5",
        "tablet:min-h-[calc(100vh-16px)] tablet:z-30 tablet:absolute tablet:rounded-lg",
        "phone:min-h-[calc(100vh-16px)] phone:z-30 phone:absolute phone:rounded-lg"
      )}
    >
      <div className="grow border-b border-border-5">
        <div className="h-[60px] px-3 py-2 flex items-center border-b border-border-5 mb-3">
          {closeButton ? (
            closeButton
          ) : (
            <Link
              to={
                isApp
                  ? `${minerRoot}/${navigationItems.home}`
                  : `${minerRoot}/${navigationItems.onboarding}`
              }
            >
              <Logo className="hover:cursor-pointer text-text-primary" />
            </Link>
          )}
        </div>
        <div className="px-3">
          {isApp && (
            <AppNavigationItems pageName={pageName} onClick={handleClick} />
          )}
          {isOnboarding && (
            <OnboardingNavigationItems
              pageName={pageName}
              onClick={handleClick}
            />
          )}
        </div>
      </div>

      <div className="px-3 pb-3">
        <VersionInfo
          loading={versionInfo?.loading}
          value={versionInfo?.value}
        />

        <MacAddressInfo loading={macInfo?.loading} value={macInfo?.value} />

        <div className="flex mt-2 space-x-2 text-emphasis-300 text-text-primary">
          <div className="w-full">
            <a href="https://proto.xyz/docs/api/v1.1.0" target="_blank">
              <Button
                variant={variants.ghost}
                size={sizes.compact}
                text="API"
                className="w-full"
              />
            </a>
          </div>
          <div className="w-full">
            <a href="mailto:mining.support@block.xyz" target="_blank">
              <Button
                variant={variants.ghost}
                size={sizes.compact}
                text="Support"
                className="w-full"
              />
            </a>
          </div>
          <Button
            variant={variants.ghost}
            size={sizes.compact}
            className="h-auto"
            onClick={() => setShowThemeSwitcher(true)}
          >
            <ThemeLight
              className="text-text-primary-30"
              width={iconSizes.small}
            />
          </Button>
          {showThemeSwitcher && (
            <ThemeSwitcher onClickDone={() => setShowThemeSwitcher(false)} />
          )}
        </div>
      </div>
    </div>
  );
};

export default Navigation;
