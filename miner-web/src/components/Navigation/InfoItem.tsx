import clsx from "clsx";

import CaretIcon from "assets/icons/caret.svg";

import SkeletonBar from "components/SkeletonBar";

import Badge, { BadgeStatus } from "./badge";

interface InfoItemProps {
  badge?: BadgeStatus;
  caret?: boolean;
  handleClick?: () => void;
  label: string;
  value?: string;
}

const InfoItem = ({
  badge,
  caret,
  handleClick,
  label,
  value,
}: InfoItemProps) => {
  return (
    <div className="text-body-regular mb-3">
      <div
        className={clsx(
          "flex items-center select-none text-foreground-100/40 tracking-[-0.28px] mb-[6px] relative",
          { "hover:cursor-pointer": caret }
        )}
        onClick={handleClick}
      >
        <div className="grow">{label}</div>
        {badge && <Badge status={badge} />}
        {caret && (
          <img src={CaretIcon} alt="caret" className="absolute right-0 top-1" />
        )}
      </div>
      <div className="text-foreground-100 tracking-[-0.14px] font-berkeley-mono-variable">
        {value || <SkeletonBar className="w-4/5 mt-1" />}
      </div>
    </div>
  );
};

export default InfoItem;
