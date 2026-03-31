import { ReactNode } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";
import SkeletonBar from "@/shared/components/SkeletonBar";

type ComponentErrorsProps = {
  icon: ReactNode;
  heading: string;
  errorCount?: number;
  href?: string;
  className?: string;
};

const ComponentErrors = ({ icon, heading, errorCount, href, className }: ComponentErrorsProps) => {
  const isLoading = errorCount === undefined;

  let statusText = "";
  if (errorCount === 0) {
    statusText = "No issues";
  } else if (errorCount === 1) {
    statusText = "1 miner needs attention";
  } else if (errorCount !== undefined) {
    statusText = `${errorCount} miners need attention`;
  }

  const content = (
    <>
      <div
        className={clsx(
          "flex h-12 w-12 justify-center rounded-lg",
          !isLoading && errorCount > 0
            ? "bg-intent-critical-fill text-text-contrast"
            : "bg-surface-5 text-text-primary-70 dark:bg-core-primary-5",
        )}
      >
        {icon}
      </div>
      <div className="flex flex-col">
        <div className="text-emphasis-300 text-text-primary">{heading}</div>
        {isLoading ? (
          <SkeletonBar className="w-32" />
        ) : (
          <div className="text-300 text-text-primary-70">{statusText}</div>
        )}
      </div>
    </>
  );

  const isClickable = href && errorCount && errorCount > 0;

  const baseClassName = clsx(
    "flex items-center gap-3 rounded-xl bg-surface-base dark:bg-core-primary-5 p-4",
    isClickable && "hover:bg-core-primary-10",
    className,
  );

  if (isClickable) {
    return (
      <Link to={href} className={baseClassName}>
        {content}
      </Link>
    );
  }

  return <div className={baseClassName}>{content}</div>;
};

export default ComponentErrors;
