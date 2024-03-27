import clsx from "clsx";
import { getTickValue } from "components/Chart";

interface HashrateTooltipItemProps {
  colorClassName: string;
  label: string;
  value: string | number;
}

const HashrateTooltipItem = ({ colorClassName, label, value }: HashrateTooltipItemProps) => {
  return (
    <div className="flex space-x-2 px-6 items-center py-2 -mt-2">
      <div className={clsx("w-1 h-3 rounded-sm", colorClassName)} />
      <div className="text-emphasis-300 text-text-primary grow">
        {label}
      </div>
      <div className="text-300 text-text-primary">
        {`${getTickValue(value)} TH/s`}
      </div>
    </div>
  );
};

export default HashrateTooltipItem;
