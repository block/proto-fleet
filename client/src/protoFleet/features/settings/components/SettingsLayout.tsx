import { ReactNode } from "react";
import SecondaryNavigation from "@/protoFleet/components/SecondaryNavigation";
import { secondaryNavItems } from "@/protoFleet/config/navItems";

const HomeLayout = ({ children }: { children?: ReactNode }) => {
  return (
    <>
      <div className="flex grow flex-row">
        <SecondaryNavigation items={secondaryNavItems} />
        <div className="min-w-0 grow p-10 phone:p-6">{children}</div>
      </div>
    </>
  );
};

export default HomeLayout;
