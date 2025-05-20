import { ReactNode } from "react";

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
  return (
    <>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <Toaster />
      </div>

      <div className="absolute top-0 left-0 flex w-full flex-row bg-surface-base">
        <div className="max-sm:hidden">
          <NavigationMenu routes={routes} />
        </div>
        <div className="flex grow flex-col">
          <PageHeader />
          <div className="relative flex grow flex-row">
            <SecondaryNavigation routes={routes} />
            <div className="flex grow justify-center">
              <div className="w-[80%] max-w-256 pt-16">{children}</div>
            </div>
          </div>
        </div>
      </div>
    </>
  );
};

export default AppLayout;
