import { ReactNode, useState } from "react";

import NavigationMenu from "../NavigationMenu";
import PageHeader from "@/protoFleet/components/PageHeader";
import { primaryNavItems } from "@/protoFleet/config/navItems";

type Props = {
  children: ReactNode;
};

const AppLayout = ({ children }: Props) => {
  const [isMenuOpen, setIsMenuOpen] = useState(false);

  return (
    <div className="absolute top-0 right-0 bottom-0 left-0 bg-surface-base">
      <div className="fixed top-0 z-50 h-fit w-16 max-sm:hidden phone:w-0 tablet:w-0">
        <NavigationMenu
          items={primaryNavItems}
          isVisible={isMenuOpen}
          closeMenu={() => setIsMenuOpen(false)}
        />
      </div>

      <div className="fixed top-0 right-0 bottom-[calc(100vh-theme(spacing.1)*15)] left-16 z-40 bg-surface-base phone:bottom-[calc(100vh-theme(spacing.1)*12)] phone:left-0 tablet:bottom-[calc(100vh-theme(spacing.1)*12)] tablet:left-0">
        <PageHeader openMenu={() => setIsMenuOpen(true)} />
      </div>

      <div className="fixed top-[calc(theme(spacing.1)*15)] right-0 bottom-0 left-16 z-20 overflow-auto bg-surface-base phone:top-[calc(theme(spacing.1)*15)] phone:left-0 tablet:top-[calc(theme(spacing.1)*12)] tablet:left-0">
        {children}
      </div>
    </div>
  );
};

export default AppLayout;
