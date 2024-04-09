import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import FloatingNavigation from "./FloatingNavigation";
import { MacAddressInfoProps } from "./MacAddressInfo";
import Navigation from "./Navigation";

interface NavigationMenuProps {
  closeMenu?: () => void;
  macInfo?: MacAddressInfoProps;
  isVisible?: boolean;
}

const NavigationMenu = ({ closeMenu, macInfo, isVisible }: NavigationMenuProps) => {
  const { isPhone, isTablet } = useWindowDimensions();

  if (isPhone || isTablet) {
    if (isVisible) {
      return <FloatingNavigation macInfo={macInfo} closeMenu={closeMenu} />;
    }
    return null;
  }

  return <Navigation macInfo={macInfo} />;
};

export default NavigationMenu;
