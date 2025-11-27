import { JSXElementConstructor, ReactElement, ReactNode } from "react";
import { ResponsiveContainer } from "recharts";
import clsx from "clsx";

interface ChartWrapperProps {
  children: ReactNode;
  height?: string;
  width?: string;
  className?: string;
}

const ChartWrapper = ({ children, height = "h-full", width = "w-full", className = "" }: ChartWrapperProps) => {
  return (
    <div className={clsx("flex", height, width, className)}>
      <ResponsiveContainer width="100%" height="100%">
        {children as ReactElement<any, string | JSXElementConstructor<any>>}
      </ResponsiveContainer>
    </div>
  );
};

export default ChartWrapper;
