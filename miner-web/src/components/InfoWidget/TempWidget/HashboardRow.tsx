import clsx from "clsx";

import Row from "components/Row";

interface HashboardRowProps {
  className?: string;
  divider?: boolean;
  label: string;
  secondaryLabel?: string;
  warn?: boolean;
}

const HashboardRow = ({
  className,
  divider = true,
  label,
  secondaryLabel,
  warn,
}: HashboardRowProps) => {
  return (
    <Row className={className} divider={divider}>
      <div className="text-emphasis-300">{label}</div>
      <div className={clsx("text-200", { "text-intent-warning-text": warn })}>
        {secondaryLabel}
      </div>
    </Row>
  );
};

export default HashboardRow;
