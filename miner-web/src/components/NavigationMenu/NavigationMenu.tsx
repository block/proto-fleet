import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import FloatingNavigation from "./FloatingNavigation";
import { MacAddressInfoProps } from "./MacAddressInfo";
import Navigation from "./Navigation";
import { VersionInfoProps } from "./VersionInfo";

interface NavigationMenuProps {
  closeMenu?: () => void;
  macInfo?: MacAddressInfoProps;
  isVisible?: boolean;
  versionInfo?: VersionInfoProps;
}

const NavigationMenu = ({
  closeMenu,
  macInfo,
  isVisible,
  versionInfo,
}: NavigationMenuProps) => {
  const { isPhone, isTablet } = useWindowDimensions();

  if (isPhone || isTablet) {
    if (isVisible) {
      return <FloatingNavigation macInfo={macInfo} closeMenu={closeMenu} />;
    }
    return null;
  }

  return <Navigation macInfo={macInfo} versionInfo={versionInfo} />;
};

export default NavigationMenu;
