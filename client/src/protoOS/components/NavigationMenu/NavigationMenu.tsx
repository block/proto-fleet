import FloatingNavigation from "./FloatingNavigation";
import type { IpAddressInfoProps } from "./InfoItem/IpAddressInfo";
import type { MacAddressInfoProps } from "./InfoItem/MacAddressInfo";
import type { MinerNameInfoProps } from "./InfoItem/MinerNameInfo";
import type { VersionInfoProps } from "./InfoItem/VersionInfo";
import Navigation from "./Navigation";
import type { NavigationMenuType } from "./types";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface NavigationMenuProps {
  closeMenu?: () => void;
  ipAddressInfo?: IpAddressInfoProps;
  macInfo?: MacAddressInfoProps;
  minerNameInfo?: MinerNameInfoProps;
  isVisible?: boolean;
  type: NavigationMenuType;
  versionInfo?: VersionInfoProps;
}

const NavigationMenu = ({
  closeMenu,
  ipAddressInfo,
  macInfo,
  minerNameInfo,
  isVisible,
  type,
  versionInfo,
}: NavigationMenuProps) => {
  const { isPhone, isTablet } = useWindowDimensions();

  if (isPhone || isTablet) {
    if (isVisible) {
      return (
        <FloatingNavigation
          ipAddressInfo={ipAddressInfo}
          macInfo={macInfo}
          minerNameInfo={minerNameInfo}
          versionInfo={versionInfo}
          closeMenu={closeMenu}
          type={type}
        />
      );
    }
    return null;
  }

  return (
    <Navigation
      ipAddressInfo={ipAddressInfo}
      macInfo={macInfo}
      minerNameInfo={minerNameInfo}
      versionInfo={versionInfo}
      type={type}
    />
  );
};

export default NavigationMenu;
