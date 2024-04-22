import clsx from "clsx";

import Row from "components/Row";

interface AsicPopoverRowProps {
  className?: string;
  label: string;
  value: string | number;
}

const AsicPopoverRow = ({
  className,
  label,
  value,
}: AsicPopoverRowProps) => {
  return (
    <Row className="flex items-center" divider={false} compact>
      <svg
        width="6"
        height="6"
        viewBox="0 0 6 6"
        className={clsx("mr-[6px]", className)}
      >
        <circle cx="3" cy="3" r="3" fill="currentColor" />
      </svg>
      <div className="text-emphasis-300 text-text-primary/90 grow">{label}</div>
      <div className="text-300 text-text-primary/90">{value}</div>
    </Row>
  );
};

export default AsicPopoverRow;
