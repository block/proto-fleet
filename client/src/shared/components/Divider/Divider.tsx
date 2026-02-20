import clsx from "clsx";

interface DividerProps {
  className?: string;
  dividerStyle?: "normal" | "thick";
}

const Divider = ({ className, dividerStyle = "normal" }: DividerProps) => {
  return (
    <div
      className={clsx("w-full border-b", dividerStyle === "thick" ? "border-border-10" : "border-border-5", className)}
    />
  );
};

export default Divider;
