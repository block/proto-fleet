import Row from "components/Row";
import SkeletonBar from "components/SkeletonBar";

export interface InfoItemProps {
  divider?: boolean;
  label: string;
  loading?: boolean;
  testId?: string;
  value?: string;
}

const InfoItem = ({
  divider,
  label,
  loading,
  testId,
  value,
}: InfoItemProps) => {
  return (
    <Row
      divider={divider}
      compact
      className="flex items-center"
      testId={testId}
    >
      <div className="grow">
        <div className="relative text-200 text-text-primary-70">{label}</div>
        <div className="font-mono text-mono-text-50 text-text-primary-30 leading-[14px]">
          {loading ? (
            <SkeletonBar className="w-2/3 !h-[14px]" />
          ) : (
            (value ?? "-")
          )}
        </div>
      </div>
    </Row>
  );
};

export default InfoItem;
