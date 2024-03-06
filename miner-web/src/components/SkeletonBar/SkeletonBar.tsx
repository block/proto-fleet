import clsx from "clsx";

interface SkeletonBarProps {
  className?: string;
  theme?: "light" | "dark";
}

const SkeletonBar = ({ className, theme = "light" }: SkeletonBarProps) => {
  return (
    <div className={clsx("h-4", className)}>
      <div
        className={clsx(
          "h-full relative isolate overflow-hidden rounded",
          "before:absolute before:inset-0 before:-translate-x-full",
          "before:animate-[shimmer_2s_infinite]",
          "before:bg-gradient-to-r before:from-transparent before:to-transparent",
          { "before:via-surface-overlay": theme === "light" },
          { "before:via-text-contrast/10": theme === "dark" }
        )}
      >
        <div
          className={clsx("h-full rounded", {
            "bg-surface-overlay": theme === "light",
            "bg-text-contrast/20": theme === "dark",
          })}
        />
      </div>
    </div>
  );
};

export default SkeletonBar;
