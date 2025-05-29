import FloatingNavigation from "@/protoFleet/components/NavigationMenu/FloatingNavigation";
import Navigation from "@/protoFleet/components/NavigationMenu/Navigation";
import { NavRoute } from "@/protoFleet/routes";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type NavigationMenuProps = {
  routes: NavRoute[];
  isVisible?: boolean;
  closeMenu?: () => void;
};

const NavigationMenu = ({
  routes,
  isVisible,
  closeMenu,
}: NavigationMenuProps) => {
  const { isPhone, isTablet } = useWindowDimensions();

  if (isPhone || isTablet) {
    if (isVisible) {
      return <FloatingNavigation routes={routes} closeMenu={closeMenu} />;
    }
    return null;
  }

  return <Navigation routes={routes} />;
};

export default NavigationMenu;
