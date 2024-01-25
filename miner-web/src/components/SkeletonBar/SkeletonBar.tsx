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
          before:from-transparent before:via-foreground-30/20 before:to-transparent
          isolate
          overflow-hidden
          shadow-xl shadow-black/5"
      >
        <div className="h-full bg-white-100/40" />
      </div>
    </div>
  );
};

export default SkeletonBar;
