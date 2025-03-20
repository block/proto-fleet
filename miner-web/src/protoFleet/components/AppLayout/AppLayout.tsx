import { ReactNode } from "react";

import NavigationMenu from "../NavigationMenu";
import SecondaryNavigation from "../SecondaryNavigation";
import PageHeader from "@/protoFleet/components/PageHeader";
import routes from "@/protoFleet/routes";

type Props = {
  children: ReactNode;
  title: string;
};

const AppLayout = ({ children, title }: Props) => {
  return (
    <div className="absolute top-0 left-0 flex w-full flex-row bg-surface-base">
      <NavigationMenu routes={routes} />
      <div className="flex grow flex-col">
        <PageHeader />
        <div>{title}</div>
        <div className="relative flex grow flex-row">
          <SecondaryNavigation routes={routes} />
          <div className="grow">{children}</div>
        </div>
      </div>
    </div>
  );
};

export default AppLayout;
