import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";

export interface InfoItemProps {
  divider?: boolean;
  label: string;
  loading?: boolean;
  testId?: string;
  value?: string;
}

const InfoItem = ({ divider, label, loading, testId, value }: InfoItemProps) => {
  return (
    <Row divider={divider} compact className="flex items-center" testId={testId}>
      <div className="grow">
        <div className="relative text-200 text-text-primary-70">{label}</div>
        <div className="font-mono text-mono-text-50 leading-[14px] text-text-primary-30">
          {loading ? <SkeletonBar className="h-[14px]! w-2/3" /> : (value ?? "—")}
        </div>
      </div>
    </Row>
  );
};

export default InfoItem;
