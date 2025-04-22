import { ReactNode } from "react";

import StatusCircle, {
  type StatusCircleProps,
} from "@/shared/components/StatusCircle";

type R2StatusIconProps = {
  status: StatusCircleProps["status"];
  icon: ReactNode;
};

const R2Status = ({ status, icon }: R2StatusIconProps) => {
  return (
    <div className="py-1.5">
      <div className="relative rounded-md bg-surface-5 p-1.5">
        <div className="absolute -top-[2px] -right-[2px]">
          <StatusCircle status={status} width="w-[10px]" removeMargin={true} />
        </div>
        {icon}
      </div>
    </div>
  );
};

export default R2Status;
