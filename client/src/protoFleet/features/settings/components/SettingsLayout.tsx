import { ReactNode } from "react";
import SecondaryNavigation from "@/protoFleet/components/SecondaryNavigation";
import { secondaryNavItems } from "@/protoFleet/config/navItems";

const HomeLayout = ({ children }: { children?: ReactNode }) => {
  return (
    <>
      <div className="flex h-full grow flex-row">
        <SecondaryNavigation items={secondaryNavItems} />
        <div className="flex min-w-0 grow flex-col p-10 phone:p-6">{children}</div>
      </div>
    </>
  );
};

export default HomeLayout;
