import TooltipComponent from ".";
import { Position, positions } from "@/shared/constants";

interface TooltipWrapperProps {
  position: Position;
}

const TooltipWrapper = ({ position }: TooltipWrapperProps) => {
  return (
    <div>
      <div className="mb-2 text-heading-100">Position: {position}</div>
      <div className="flex w-80">
        <TooltipComponent header="Tooltip Header" body="Tooltip Body" position={position} />
      </div>
    </div>
  );
};

export const Tooltip = () => {
  return (
    <div className="mt-4 ml-4 flex flex-col space-y-4">
      <div className="flex">
        <TooltipWrapper position={positions["bottom right"]} />
        <TooltipWrapper position={positions["bottom left"]} />
      </div>
      <div className="flex">
        <TooltipWrapper position={positions["top right"]} />
        <TooltipWrapper position={positions["top left"]} />
      </div>
    </div>
  );
};

export default {
  title: "Shared/Tooltip",
};
