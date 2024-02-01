import clsx from "clsx";

import CaretIcon from "assets/icons/caret.svg";

import SkeletonBar from "components/SkeletonBar";

import Badge, { BadgeStatus } from "./badge";

interface InfoItemProps {
  badge?: BadgeStatus;
  caret?: boolean;
  error?: boolean;
  handleClick?: () => void;
  label: string;
  value?: string;
}

const InfoItem = ({
  badge,
  caret,
  error,
  handleClick,
  label,
  value,
}: InfoItemProps) => {
  return (
    <div className="text-body-regular mb-3">
      <div
        className={clsx(
          "flex items-center select-none tracking-[-0.28px] mb-[6px] relative",
          { "hover:cursor-pointer": caret },
          { "text-foreground-100/40": !error },
          { "text-critical-100": error }
        )}
        onClick={handleClick}
      >
        <div className="grow">{label}</div>
        {badge && <Badge status={badge} />}
        {caret && (
          <img src={CaretIcon} alt="caret" className="absolute right-0 top-1" />
        )}
      </div>
      <div
        className={clsx(
          "tracking-[-0.14px] font-mono",
          { "text-foreground-100": !error },
          { "text-critical-100": error }
        )}
      >
        {value || <SkeletonBar className="w-4/5 mt-1" />}
      </div>
    </div>
  );
};

export default InfoItem;
