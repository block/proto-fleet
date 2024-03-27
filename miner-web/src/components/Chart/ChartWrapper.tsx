import { JSXElementConstructor, ReactElement, ReactNode, RefObject } from "react";
import { ResponsiveContainer } from "recharts";

interface ChartWrapperProps {
  children: ReactNode;
  tooltipRef: RefObject<HTMLDivElement>;
}

const ChartWrapper = ({ children, tooltipRef }: ChartWrapperProps) => {
  return (
    <div ref={tooltipRef} className="flex w-full h-full">
      <ResponsiveContainer width="100%" height="100%">
        {children as ReactElement<any, string | JSXElementConstructor<any>>}
      </ResponsiveContainer>
    </div>
  );
};

export default ChartWrapper;
