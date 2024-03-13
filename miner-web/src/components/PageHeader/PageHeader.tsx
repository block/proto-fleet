import PoolStatus from "./PoolStatus";

interface PageHeaderProps {
  title: string;
}

const PageHeader = ({ title }: PageHeaderProps) => {
  return (
    <div className="h-[56px] flex border-b border-border-primary/5 py-2 p-[15px] items-center">
      <div className="text-300 text-text-primary/70 grow">{title}</div>
      {/* TODO: add errors & warnings here */}
      <PoolStatus />
    </div>
  );
};

export default PageHeader;
