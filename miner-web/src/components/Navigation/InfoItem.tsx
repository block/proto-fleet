import clsx from "clsx";

import CaretIcon from "assets/caret.svg";

import SkeletonBar from "components/SkeletonBar";

interface InfoItemProps {
  caret?: boolean;
  handleClick?: () => void;
  label: string;
  value?: string;
}

const InfoItem = ({ caret, handleClick, label, value }: InfoItemProps) => {
  return (
    <div className="text-body-default mb-3 select-none">
      <div
        className={clsx("tracking-[-0.28px]", { "hover:cursor-pointer": caret })}
        onClick={handleClick}
      >
        {label}
        {caret && <img src={CaretIcon} alt="caret" className="inline ml-1" />}
      </div>
      <div className="tracking-[-0.14px] opacity-50">
        {value || <SkeletonBar className="w-4/5 mt-1" />}
      </div>
    </div>
  );
};

export default InfoItem;
