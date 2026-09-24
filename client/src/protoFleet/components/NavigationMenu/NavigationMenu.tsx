import type { NavigationRail } from "./useNavigationRail";
import FloatingNavigation from "@/protoFleet/components/NavigationMenu/FloatingNavigation";
import Navigation from "@/protoFleet/components/NavigationMenu/Navigation";
import { NavItem } from "@/protoFleet/config/navItems";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type NavigationMenuProps = {
  items: NavItem[];
  isVisible?: boolean;
  closeMenu?: () => void;
  rail?: NavigationRail;
};

const NavigationMenu = ({ items, isVisible, closeMenu, rail }: NavigationMenuProps) => {
  const { isPhone } = useWindowDimensions();

  if (isVisible && isPhone) {
    return <FloatingNavigation items={items} closeMenu={closeMenu} />;
  }

  if (isPhone) return null;

  return <Navigation items={items} rail={rail} />;
};

export default NavigationMenu;
