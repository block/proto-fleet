import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";

export interface LocationInfoProps {
  loading?: boolean;
  // Placement levels (site, building, rack), already filtered to non-empty
  // values by the caller. Rendered stacked under a single "Location" label.
  values?: string[];
}

const LocationInfo = ({ loading, values = [] }: LocationInfoProps) => {
  return (
    <Row divider={false} compact className="flex items-center" testId="location-info-item">
      <div className="grow">
        <div className="relative text-200 text-text-primary-70">Location</div>
        <div className="font-mono text-mono-text-50 leading-[16px] text-text-primary-30">
          {loading ? (
            <SkeletonBar className="h-[14px]! w-2/3" />
          ) : values.length ? (
            values.map((line, index) => <div key={`${index}-${line}`}>{line}</div>)
          ) : (
            "—"
          )}
        </div>
      </div>
    </Row>
  );
};

export default LocationInfo;
