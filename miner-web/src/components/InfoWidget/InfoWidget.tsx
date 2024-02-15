import clsx from "clsx";

import SkeletonBar from "components/SkeletonBar";

interface InfoWidgetProps {
  className?: string;
  loading?: boolean;
  title: string;
  value?: string | number;
}

const InfoWidget = ({ className, loading, title, value }: InfoWidgetProps) => {
  return (
    <div className="min-w-[250px]">
      <div className="text-emphasis-400 text-text-primary/70 mb-2">
        {title}
      </div>
      <div className={clsx("text-heading-300 font-mono", className)}>
        {loading ? (
          <SkeletonBar className="w-36 mt-4" />
        ) : (
          <>{value ?? "-"}</>
        )}
      </div>
    </div>
  );
};

export default InfoWidget;
