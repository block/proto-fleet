import FloatingNavigation from "@/protoFleet/components/NavigationMenu/FloatingNavigation";
import Navigation from "@/protoFleet/components/NavigationMenu/Navigation";
import { NavItem } from "@/protoFleet/config/navItems";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type NavigationMenuProps = {
  items: NavItem[];
  isVisible?: boolean;
  closeMenu?: () => void;
};

const NavigationMenu = ({ items, isVisible, closeMenu }: NavigationMenuProps) => {
  const { isPhone, isTablet } = useWindowDimensions();

  if (isPhone || isTablet) {
    if (isVisible) {
      return <FloatingNavigation items={items} closeMenu={closeMenu} />;
    }
    return null;
  }

  return <Navigation items={items} />;
};

export default NavigationMenu;
