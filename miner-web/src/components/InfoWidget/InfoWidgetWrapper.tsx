import { ReactNode } from "react";

interface InfoWidgetWrapperProps {
  children: ReactNode;
}

const InfoWidgetWrapper = ({ children }: InfoWidgetWrapperProps) => {
  return <div className="flex justify-between">{children}</div>;
};

export default InfoWidgetWrapper;
