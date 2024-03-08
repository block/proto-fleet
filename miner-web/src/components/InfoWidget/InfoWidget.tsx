import { ReactNode } from "react";
import clsx from "clsx";

import EmptyValue from "components/EmptyValue";
import SkeletonBar from "components/SkeletonBar";

interface InfoWidgetProps {
  hasBorder?: boolean;
  loading?: boolean;
  stats?: ReactNode;
  title: string;
  value?: string | number;
}

const InfoWidget = ({ hasBorder, loading, stats, title, value }: InfoWidgetProps) => {
  return (
    <div
      className={clsx("p-4 grow basis-0 flex", {
        "border border-border-primary/5 rounded-xl": hasBorder,
      })}
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
    </div>
  );
};

export default InfoWidget;
