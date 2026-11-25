import { ReactNode } from "react";

type SectionHeadingProps = {
  heading: string;
  children?: ReactNode;
};

const SectionHeading = ({ heading, children }: SectionHeadingProps) => {
  return (
    <div className="flex flex-col gap-4 tablet:flex-row tablet:items-center tablet:justify-between">
      <div className="text-emphasis-400 text-text-primary">{heading}</div>
      {children ? <div className="flex items-center">{children}</div> : null}
    </div>
  );
};

export default SectionHeading;
