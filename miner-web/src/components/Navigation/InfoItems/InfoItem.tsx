import clsx from "clsx";

import Row from "components/Row";
import SkeletonBar from "components/SkeletonBar";

import Badge, { BadgeStatus } from "../badge";

interface InfoItemProps {
  badge?: BadgeStatus;
  error?: boolean;
  label: string;
  loading?: boolean;
  value?: string | number;
}

const InfoItem = ({
  badge,
  error,
  label,
  loading,
  value,
}: InfoItemProps) => {
  return (
    <Row compact divider={false} className="flex items-center">
      <div className="grow">
        <div
          className={clsx(
            "relative text-200",
            { "text-text-contrast/70": !error },
            { "text-text-critical": error }
          )}
        >
          {label}
        </div>
        <div
          className={clsx(
            "font-mono text-mono-text-50",
            { "text-text-contrast/70": !error },
            { "text-text-critical": error }
          )}
        >
          {loading ? (
            <SkeletonBar className="w-4/5" theme="dark" />
          ) : (
            value ?? "-"
          )}
        </div>
      </div>
      {badge && <Badge status={badge} />}
    </Row>
  );
};

export default InfoItem;
