import clsx from "clsx";

import SkeletonBar from "components/SkeletonBar";

import CaretIcon from "icons/Caret";

import Badge, { BadgeStatus } from "../badge";

interface InfoItemProps {
  badge?: BadgeStatus;
  caret?: boolean;
  error?: boolean;
  onClick?: () => void;
  label: string;
  loading?: boolean;
  value?: string | number;
}

const InfoItem = ({
  badge,
  caret,
  error,
  onClick,
  label,
  loading,
  value,
}: InfoItemProps) => {
  return (
    <div className="text-200 mb-3">
      <div
        className={clsx(
          "flex items-center select-none mb-[6px] relative",
          { "hover:cursor-pointer": caret },
          { "text-text-primary/40": !error },
          { "text-text-critical": error }
        )}
        onClick={onClick}
      >
        <div className="grow">{label}</div>
        {badge && <Badge status={badge} />}
        {caret && <CaretIcon className="absolute right-0 top-1" />}
      </div>
      <div
        className={clsx(
          "font-mono",
          { "text-text-primary": !error },
          { "text-text-critical": error }
        )}
      >
        {loading ? <SkeletonBar className="w-4/5 mt-1" /> : value ?? "-"}
      </div>
    </div>
  );
};

export default InfoItem;
