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
        <NavigationMenu
          routes={routes}
          isVisible={isMenuOpen}
          closeMenu={() => setIsMenuOpen(false)}
        />
        <div className="flex grow flex-col">
          <PageHeader openMenu={() => setIsMenuOpen(true)} />
          <div className="relative flex grow flex-row">
            <SecondaryNavigation routes={routes} />
            <div className="flex grow justify-center p-20 phone:p-6 tablet:p-6">
              <div className="phone:w-[352px] tablet:w-[584px] laptop:w-[776px] desktop:w-[1024px]">
                {children}
              </div>
            </div>
          </div>
        </div>
      </div>
    </>
  );
};

export default AppLayout;
