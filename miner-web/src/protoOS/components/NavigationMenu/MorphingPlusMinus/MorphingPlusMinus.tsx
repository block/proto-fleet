import clsx from "clsx";

const sharedCls =
  "bg-text-primary absolute top-1/2 left-1/2 bg-[var(--typography-primary-70)] -translate-x-1/2 -translate-y-1/2";

const MorphingPlusMinus = ({ condition }: { condition: boolean }) => {
  return (
    <div className={clsx("relative h-[20px] w-[20px] opacity-30")}>
      <div
        className={clsx(
          sharedCls,
          "h-[10px] w-[2px] transition-transform duration-300 ease-gentle",
          condition ? "scale-y-100" : "scale-y-0",
        )}
      ></div>
      <div className={clsx(sharedCls, "h-[2px] w-[10px]")}></div>
    </div>
  );
};

export default MorphingPlusMinus;
