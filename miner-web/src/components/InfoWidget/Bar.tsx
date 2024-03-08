import clsx from "clsx";

interface BarProps {
  intensity: number;
}

const Bar = ({ intensity }: BarProps) => {
  return (
    <div className="flex flex-col space-y-1">
      {[...Array(10)].map((_, index) => {
        const isFilled = index >= 10 - intensity;
        return (
          <div
            key={index}
            className={clsx("w-4 h-[2.8px] rounded", {
              "bg-border-primary/5": !isFilled,
              // TODO: figure out if higher number means good/bad
              "bg-intent-success-fill": isFilled && intensity <= 4,
              "bg-intent-warning-fill": isFilled && intensity >= 5 && intensity <= 7,
              "bg-intent-critical-fill": isFilled && intensity >= 8,
            })}
          />
        );
      })}
    </div>
  );
};

export default Bar;
