import clsx from "clsx";

import EmptyValue from "components/EmptyValue";
import Row from "components/Row";
import SkeletonBar from "components/SkeletonBar";

interface HashboardRowProps {
  className?: string;
  divider?: boolean;
  label: string;
  loading?: boolean;
  secondaryLabel?: string;
  warn?: boolean;
}

const HashboardRow = ({
  className,
  divider = true,
  label,
  loading,
  secondaryLabel,
  warn,
}: HashboardRowProps) => {
  return (
    <Row className={className} divider={divider}>
      <div className="text-emphasis-300">{label}</div>
      <div className={clsx("text-200", { "text-intent-warning-text": warn })}>
        {loading ? (
            <SkeletonBar className="w-10 mt-1" />
          ) : (
            <>{secondaryLabel ?? <EmptyValue className="!h-2 mt-1" />}</>
          )}
      </div>
    </Row>
  );
};

export default HashboardRow;
