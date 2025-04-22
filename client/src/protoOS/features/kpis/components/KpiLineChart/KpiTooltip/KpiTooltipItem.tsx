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
    <div className="-mt-2 flex items-center space-x-2 px-6 py-2">
      <div
        className={clsx("h-3 w-1 rounded-xs")}
        style={{ background: color }}
      />
      <div className="grow text-emphasis-300 text-text-primary">{label}</div>
      <div className="text-300 text-text-primary">
        {value} {units && <span>{units}</span>}
      </div>
    </div>
  );
};

export default KpiTooltipItem;
