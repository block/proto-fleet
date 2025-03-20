import clsx from "clsx";

import EmptyValue from "@/shared/components/EmptyValue";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";

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
      <div className="text-emphasis-300 text-text-primary">{label}</div>
      <div
        className={clsx("text-200", {
          "text-intent-warning-text": warn,
          "text-text-primary-70": !warn,
        })}
      >
        {loading ? (
          <SkeletonBar className="mt-1 w-10" />
        ) : (
          <>{secondaryLabel ?? <EmptyValue className="mt-1 h-2!" />}</>
        )}
      </div>
    </Row>
  );
};

export default HashboardRow;
