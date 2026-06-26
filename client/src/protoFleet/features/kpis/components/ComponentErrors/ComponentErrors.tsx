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
      <div className="flex h-12 w-12 justify-center rounded-lg bg-intent-critical-fill text-text-contrast">{icon}</div>
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
    // Contrasting card surface, matching the rack grid cards.
    "flex items-center gap-3 rounded-xl bg-surface-overlay p-4",
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
