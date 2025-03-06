import clsx from "clsx";

interface EmptyValueProps {
  className?: string;
}

const EmptyValue = ({ className }: EmptyValueProps) => {
  return (
    <div
      className={clsx("h-10 flex items-center", className)}
      data-testid="empty-value"
    >
      <div className="w-6 h-1 bg-core-primary-10 rounded-xl" />
    </div>
  );
};

export default EmptyValue;
