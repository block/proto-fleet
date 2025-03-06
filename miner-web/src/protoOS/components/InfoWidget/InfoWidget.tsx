import { ReactNode } from "react";
import clsx from "clsx";

import EmptyValue from "@/shared/components/EmptyValue";
import SkeletonBar from "@/shared/components/SkeletonBar";

interface InfoWidgetProps {
  className?: string;
  hasBorder?: boolean;
  loading?: boolean;
  onClick?: () => void;
  stats?: ReactNode;
  title: string;
  value?: string | number | null;
  wrapperClassName?: string;
}

const InfoWidget = ({
  className,
  hasBorder,
  loading,
  onClick,
  stats,
  title,
  value,
  wrapperClassName,
}: InfoWidgetProps) => {
  const Element = onClick ? "button" : "div";
  return (
    <Element
      className={clsx(
        "group text-left relative grow basis-0 flex transition-[background-color] ease-in-out duration-200",
        {
          "p-4 border border-border-5 rounded-xl": hasBorder,
          "hover:bg-core-primary-5": onClick,
        },
        wrapperClassName,
      )}
      onClick={onClick}
      data-testid="info-widget"
    >
      <div
        className={clsx("whitespace-nowrap", { "grow mr-2": stats }, className)}
      >
        <div className="text-heading-50 text-text-primary-50 mb-1">{title}</div>
        <div className="text-heading-300 text-text-primary">
          {loading ? (
            <SkeletonBar className="w-36 mt-4" />
          ) : (
            <>{value ?? <EmptyValue />}</>
          )}
        </div>
      </div>
      {stats}
    </Element>
  );
};

export default InfoWidget;
