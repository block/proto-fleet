import { ReactNode } from "react";

import NavigationMenu from "../NavigationMenu";

type Props = {
  children: ReactNode;
  title: string;
};

const AppLayout = ({ children, title }: Props) => {
  return (
    <div className="absolute top-0 left-0 w-full flex flex-row bg-surface-base">
      <NavigationMenu />
      <div className="grow flex flex-col">
        <div className="h-[60px] border-b border-border-5 flex items-center justify-center">
          {title}
        </div>
        <div className="grow relative">{children}</div>
      </div>
    </div>
  );
};

export default AppLayout;
