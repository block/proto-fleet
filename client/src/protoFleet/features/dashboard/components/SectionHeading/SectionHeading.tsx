import { ReactNode } from "react";
import clsx from "clsx";

type SectionHeadingProps = {
  heading: string;
  children?: ReactNode;
  className?: string;
};

const SectionHeading = ({ heading, children, className }: SectionHeadingProps) => {
  return (
    <div className={clsx("flex flex-col gap-4 tablet:flex-row tablet:items-center tablet:justify-between", className)}>
      <div className="text-emphasis-400 text-text-primary">{heading}</div>
      {children ? <div className="flex items-center">{children}</div> : null}
    </div>
  );
};

export default SectionHeading;
