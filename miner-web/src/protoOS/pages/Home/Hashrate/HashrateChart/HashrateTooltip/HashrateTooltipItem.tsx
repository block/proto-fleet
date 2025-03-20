import clsx from "clsx";

interface HashrateTooltipItemProps {
  colorClassName: string;
  label: string;
  value?: string | number;
}

const HashrateTooltipItem = ({
  colorClassName,
  label,
  value,
}: HashrateTooltipItemProps) => {
  if (!value) return null;

  return (
    <div className="-mt-2 flex items-center space-x-2 px-6 py-2">
      <div className={clsx("h-3 w-1 rounded-xs", colorClassName)} />
      <div className="grow text-emphasis-300 text-text-primary">{label}</div>
      <div className="text-300 text-text-primary">{value} TH/s</div>
    </div>
  );
};

export default HashrateTooltipItem;
