import { ReactNode } from "react";
import clsx from "clsx";

import EmptyValue from "components/EmptyValue";
import SkeletonBar from "components/SkeletonBar";

interface InfoWidgetProps {
  hasBorder?: boolean;
  loading?: boolean;
  onClick?: () => void;
  stats?: ReactNode;
  title: string;
  value?: string | number | null;
}

const InfoWidget = ({
  hasBorder,
  loading,
  onClick,
  stats,
  title,
  value,
}: InfoWidgetProps) => {
  const Element = onClick ? "button" : "div";
  return (
    <Element
      className={clsx("grow basis-0 flex text-left w-full", {
        "p-4 border border-border-primary/5 rounded-xl": hasBorder,
      })}
      onClick={onClick}
      data-testid="info-widget"
    >
      <div className="grow mr-2 whitespace-nowrap">
        <div className="text-heading-50 text-text-primary/50 mb-1">{title}</div>
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
