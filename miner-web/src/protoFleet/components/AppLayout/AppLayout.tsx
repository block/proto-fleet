import { ReactNode } from "react";

import NavigationMenu from "../NavigationMenu";
import SecondaryNavigation from "../SecondaryNavigation";
import routes from "@/protoFleet/routes";

type Props = {
  children: ReactNode;
  title: string;
};

const AppLayout = ({ children, title }: Props) => {
  return (
    <div className="absolute top-0 left-0 w-full flex flex-row bg-surface-base">
      <NavigationMenu routes={routes} />
      <div className="grow flex flex-col">
        <div className="h-[60px] border-b border-border-5 flex items-center justify-center">
          {title}
        </div>
        <div className="grow relative flex flex-row">
          <SecondaryNavigation routes={routes} />
          <div className="grow">{children}</div>
        </div>
      </div>
    </div>
  );
};

export default AppLayout;
