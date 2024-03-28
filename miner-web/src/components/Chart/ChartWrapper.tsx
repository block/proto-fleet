import { JSXElementConstructor, ReactElement, ReactNode } from "react";
import { ResponsiveContainer } from "recharts";

interface ChartWrapperProps {
  children: ReactNode;
}

const ChartWrapper = ({ children }: ChartWrapperProps) => {
  return (
    <div className="flex w-full h-full">
      <ResponsiveContainer width="100%" height="100%">
        {children as ReactElement<any, string | JSXElementConstructor<any>>}
      </ResponsiveContainer>
    </div>
  );
};

export default ChartWrapper;
