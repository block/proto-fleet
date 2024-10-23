import clsx from "clsx";

interface DividerProps {
  className?: string;
}

const Divider = ({ className }: DividerProps) => {
  return <div className={clsx("border-b w-full border-border-5", className)} />;
};

export default Divider;
