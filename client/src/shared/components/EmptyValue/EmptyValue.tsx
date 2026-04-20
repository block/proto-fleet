import clsx from "clsx";

interface EmptyValueProps {
  className?: string;
}

const EmptyValue = ({ className }: EmptyValueProps) => {
  return (
    <div className={clsx("flex h-10 items-center", className)} data-testid="empty-value">
      <div className="h-1 w-6 rounded-xl bg-core-primary-10" />
    </div>
  );
};

export default EmptyValue;
