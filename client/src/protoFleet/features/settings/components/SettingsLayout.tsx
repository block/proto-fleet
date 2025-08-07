import { ReactNode } from "react";
import SecondaryNavigation from "@/protoFleet/components/SecondaryNavigation";
import routes from "@/protoFleet/routes";

const HomeLayout = ({ children }: { children?: ReactNode }) => {
  return (
    <>
      <div className="flex grow flex-row">
        <SecondaryNavigation routes={routes} />
        <div className="grow p-10 phone:p-6">{children}</div>
      </div>
    </>
  );
};

export default HomeLayout;
