import { ReactNode } from "react";

import NavigationMenu from "../NavigationMenu";
import SecondaryNavigation from "../SecondaryNavigation";
import PageHeader from "@/protoFleet/components/PageHeader";
import routes from "@/protoFleet/routes";

type Props = {
  children: ReactNode;
  title: string;
};

const AppLayout = ({ children }: Props) => {
  return (
    <div className="absolute top-0 left-0 flex w-full flex-row bg-surface-base">
      <NavigationMenu routes={routes} />
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
  );
};

export default AppLayout;
