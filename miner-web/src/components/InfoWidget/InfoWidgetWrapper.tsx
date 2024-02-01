import { ReactNode } from "react";
import clsx from "clsx";

interface InfoWidgetWrapperProps {
  className?: string;
  children: ReactNode;
}

const InfoWidgetWrapper = ({ children, className }: InfoWidgetWrapperProps) => {
  return (
    <div className={clsx("flex space-x-6", className)}>{children}</div>
  );
};

export default InfoWidgetWrapper;
