import clsx from "clsx";

interface AsicChartTooltipItemProps {
  colorClassName: string;
  label: string;
  value: string | number;
}

const AsicChartTooltipItem = ({ colorClassName, label, value }: AsicChartTooltipItemProps) => {
  return (
    <div className="flex space-x-2 items-center py-2 -mt-2">
      <div className={clsx("w-1 h-3 rounded-sm", colorClassName)} />
      <div className="text-emphasis-300 text-text-primary grow">
        {label}
      </div>
      <div className="text-300 text-text-primary">
        {value}
      </div>
    </div>
  );
};

export default AsicChartTooltipItem;
