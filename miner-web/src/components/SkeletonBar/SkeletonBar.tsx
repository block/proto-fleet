import clsx from "clsx";

interface SkeletonBarProps {
  className?: string;
}

const SkeletonBar = ({ className }: SkeletonBarProps) => {
  return (
    <div className={clsx("h-4 rounded-md", className)}>
      <div
        className="h-full relative
          before:absolute before:inset-0
          before:-translate-x-full
          before:animate-[shimmer_2s_infinite]
          before:bg-gradient-to-r
          before:from-transparent before:via-surface-overlay before:to-transparent
          isolate
          overflow-hidden"
      >
        <div className="h-full bg-surface-overlay" />
      </div>
    </div>
  );
};

export default SkeletonBar;
