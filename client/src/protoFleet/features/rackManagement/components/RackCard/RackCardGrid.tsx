import type { ReactNode } from "react";

interface RackCardGridProps {
  children: ReactNode;
}

const RackCardGrid = ({ children }: RackCardGridProps) => (
  <div className="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-3">{children}</div>
);

export default RackCardGrid;
