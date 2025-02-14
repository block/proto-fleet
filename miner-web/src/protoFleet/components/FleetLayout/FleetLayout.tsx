import { ReactNode } from "react";

import SecondaryNavigation from "@/protoFleet/components/SecondaryNavigation";

const items = [
  {
    name: "Containers",
    route: "/containers",
  },
  {
    name: "Racks",
    route: "/racks",
  },
  {
    name: "Miners",
    route: "/miners",
  },
];

type Props = {
  children: ReactNode;
};

const FleetLayout = ({ children }: Props) => {
  return (
    <div className="flex h-full">
      <SecondaryNavigation items={items} />
      <div className="grow relative">
        <div className="p-8">{children}</div>
      </div>
    </div>
  );
};

export default FleetLayout;
