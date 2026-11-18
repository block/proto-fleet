import clsx from "clsx";

type SettingsEmptyStateProps = {
  title: string;
  description?: string;
  className?: string;
  size?: "default" | "section";
};

const SettingsEmptyState = ({ title, description, className, size = "default" }: SettingsEmptyStateProps) => (
  <div
    className={clsx(
      "flex w-full flex-col items-center justify-center text-center",
      size === "section" ? "min-h-[220px] py-14" : "py-10",
      className,
    )}
  >
    <div className="text-heading-200 text-text-primary">{title}</div>
    {description ? <p className="mt-1 text-400 text-text-primary-70">{description}</p> : null}
  </div>
);

export default SettingsEmptyState;
