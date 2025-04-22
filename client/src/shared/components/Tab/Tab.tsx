import { ReactNode } from "react";

interface TabProps {
  children: ReactNode;
  className?: string;
  label: string;
}

const Tab = ({ children }: TabProps) => {
  return <>{children}</>;
};

export default Tab;
