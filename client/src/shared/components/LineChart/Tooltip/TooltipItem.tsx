import { ComponentType } from "react";

import { Circle } from "@/shared/assets/icons";
import useCssVariable from "@/shared/hooks/useCssVariable";

interface TooltipItemProps {
  itemKey: string;
  colorMap?: { [key: string]: string };
  value?: string | number;
  units?: string;
  icon?: ComponentType<{ itemKey: string }>;
}

const TooltipItem = ({ itemKey, value, units, icon: Icon, colorMap }: TooltipItemProps) => {
  const color = useCssVariable(colorMap?.[itemKey] || "--color-bg-core-primary-5");

  if (!value) return null;

  return (
    <>
      <div className="-mt-2 flex items-center justify-between space-x-3 py-2">
        <div className="inline-flex items-center gap-2">
          <Circle style={{ backgroundColor: color }} width="w-2" />
          <div className="grow text-end text-300 text-text-primary">
            {value} {units && <span>{units}</span>}
          </div>
        </div>

        {Icon && <Icon itemKey={itemKey} />}
      </div>
    </>
  );
};

export default TooltipItem;
