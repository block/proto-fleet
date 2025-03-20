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
        "group relative flex grow basis-0 text-left transition-[background-color] duration-200 ease-in-out",
        {
          "rounded-xl border border-border-5 p-4": hasBorder,
          "hover:bg-core-primary-5": onClick,
        },
        wrapperClassName,
      )}
      onClick={onClick}
      data-testid="info-widget"
    >
      <div
        className={clsx("whitespace-nowrap", { "mr-2 grow": stats }, className)}
      >
        <div className="mb-1 text-heading-50 text-text-primary-50">{title}</div>
        <div className="text-heading-300 text-text-primary">
          {loading ? (
            <SkeletonBar className="mt-4 w-36" />
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
