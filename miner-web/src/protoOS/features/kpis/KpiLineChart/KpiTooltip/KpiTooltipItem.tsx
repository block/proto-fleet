import clsx from "clsx";

interface KpiTooltipItemProps {
  color?: string;
  label: string;
  value?: string | number;
  units?: string;
}

const KpiTooltipItem = ({
  color,
  label,
  value,
  units,
}: KpiTooltipItemProps) => {
  if (!value) return null;

  return (
    <div className="flex space-x-2 px-6 items-center py-2 -mt-2">
      <div
        className={clsx("w-1 h-3 rounded-xs")}
        style={{ background: color }}
      />
      <div className="text-emphasis-300 text-text-primary grow">{label}</div>
      <div className="text-300 text-text-primary">
        {value} {units && <span>{units}</span>}
      </div>
    </div>
  );
};

export default KpiTooltipItem;
