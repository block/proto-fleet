import { ReactNode } from "react";
import clsx from "clsx";

type SectionHeadingProps = {
  heading: string;
  children?: ReactNode;
  className?: string;
};

const SectionHeading = ({ heading, children, className }: SectionHeadingProps) => {
  return (
    <div className={clsx("flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between", className)}>
      <div className="text-emphasis-400 text-text-primary">{heading}</div>
      {children ? <div className="flex items-center">{children}</div> : null}
    </div>
  );
};

export default SectionHeading;
