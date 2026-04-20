import clsx from "clsx";

interface SkeletonBarProps {
  className?: string;
}

const SkeletonBar = ({ className }: SkeletonBarProps) => {
  return (
    <div className={clsx("h-4", className)} data-testid="skeleton-bar">
      <div
        className={clsx(
          "relative isolate h-full overflow-hidden rounded-sm",
          "before:absolute before:inset-0",
          "before:animate-[shimmer_2s_ease-in-out_infinite]",
          "before:bg-[linear-gradient(90deg,transparent_0%,var(--color-core-primary-5)_30%,var(--color-core-primary-5)_70%,transparent_100%)]",
        )}
      >
        <div className="h-full rounded-sm bg-core-primary-10" />
      </div>
    </div>
  );
};

export default SkeletonBar;
