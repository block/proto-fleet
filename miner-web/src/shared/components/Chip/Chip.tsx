import { ReactNode } from "react";
import Spinner from "@/shared/components/Spinner";

interface ChipProps {
  loading?: boolean;
  prefixIcon?: ReactNode;
  children?: ReactNode;
}

const Chip = ({ loading, prefixIcon, children }: ChipProps) => {
  const prefix = loading ? <Spinner size={16} /> : prefixIcon;

  return (
    <div className="flex w-fit items-center rounded border border-border-5 px-2 py-1">
      {prefix}
      {children && prefix && <span className="w-1" />}
      <span className="text-200">{children}</span>
    </div>
  );
};

export default Chip;
