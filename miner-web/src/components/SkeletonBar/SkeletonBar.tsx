import clsx from "clsx";

interface SkeletonBarProps {
  className?: string;
}

const SkeletonBar = ({ className }: SkeletonBarProps) => {
  return (
    <div className={clsx("h-4", className)} data-testid="skeleton-bar">
      <div
        className={clsx(
          "h-full relative isolate overflow-hidden rounded",
          "before:absolute before:inset-0 before:-translate-x-full",
          "before:animate-[shimmer_2s_infinite]",
          "before:bg-gradient-to-r before:from-transparent before:to-transparent",
          "before:via-core-primary-5"
        )}
      >
        <div className="h-full rounded bg-core-primary-10" />
      </div>
    </div>
  );
};

export default SkeletonBar;
