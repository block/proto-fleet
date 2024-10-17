import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import FloatingNavigation from "./FloatingNavigation";
import { MacAddressInfoProps } from "./InfoItem/MacAddressInfo";
import { VersionInfoProps } from "./InfoItem/VersionInfo";
import Navigation from "./Navigation";
import { NavigationMenuType } from "./types";

interface NavigationMenuProps {
  closeMenu?: () => void;
  macInfo?: MacAddressInfoProps;
  isVisible?: boolean;
  type: NavigationMenuType;
  versionInfo?: VersionInfoProps;
}

const NavigationMenu = ({
  closeMenu,
  macInfo,
  isVisible,
  type,
  versionInfo,
}: NavigationMenuProps) => {
  const { isPhone, isTablet } = useWindowDimensions();

  if (isPhone || isTablet) {
    if (isVisible) {
      return (
        <FloatingNavigation
          macInfo={macInfo}
          versionInfo={versionInfo}
          closeMenu={closeMenu}
          type={type}
        />
      );
    }
    return null;
  }

  return <Navigation macInfo={macInfo} versionInfo={versionInfo} type={type} />;
};

export default NavigationMenu;
