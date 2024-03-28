import PoolStatus from "./PoolStatus";
import Warning from "./Warning";

interface PageHeaderProps {
  title: string;
}

const PageHeader = ({ title }: PageHeaderProps) => {
  return (
    <div className="h-[56px] flex border-b border-border-primary/5 py-2 p-[15px] items-center">
      <div className="text-300 text-text-primary/70 grow">{title}</div>
      {/* TODO: add errors & warnings from API when available */}
      <div className="flex space-x-4">
        <Warning label="ASIC" state="critical" messages={["12% Higher Temperature"]} />
        <Warning label="Fans" state="warning" messages={["Fan 1 low speed", "Fan 2 low speed"]} />
        <PoolStatus />
      </div>
    </div>
  );
};

export default PageHeader;
