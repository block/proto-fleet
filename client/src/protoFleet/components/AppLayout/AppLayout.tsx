import { ReactNode, useState } from "react";

import NavigationMenu from "../NavigationMenu";
import SecondaryNavigation from "../SecondaryNavigation";
import PageHeader from "@/protoFleet/components/PageHeader";
import routes from "@/protoFleet/routes";
import { Toaster } from "@/shared/features/toaster";

type Props = {
  children: ReactNode;
  title: string;
};

const AppLayout = ({ children }: Props) => {
  const [isMenuOpen, setIsMenuOpen] = useState(false);

  return (
    <>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <Toaster />
      </div>

      <div className="absolute top-0 left-0 flex w-full flex-row bg-surface-base">
        <div className="sticky top-0 z-50 h-fit max-sm:hidden">
          <NavigationMenu
            routes={routes}
            isVisible={isMenuOpen}
            closeMenu={() => setIsMenuOpen(false)}
          />
        </div>
        <div className="flex grow flex-col">
          <div className="sticky top-0 z-40 bg-surface-base">
            <PageHeader openMenu={() => setIsMenuOpen(true)} />
          </div>
          <div className="relative flex grow flex-row">
            <SecondaryNavigation routes={routes} />
            <div className="w-full p-20 phone:p-6 tablet:p-6">{children}</div>
          </div>
        </div>
      </div>
    </>
  );
};

export default AppLayout;
