import CopyableValue from "../CopyableValue";
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
    <Row divider={false} compact className="group/copyable flex items-center" testId="location-info-item">
      <div className="min-w-0 grow">
        <div className="relative text-200 text-text-primary-70">Location</div>
        <div className="font-mono text-mono-text-50 leading-[16px] text-text-primary-30">
          {loading ? (
            <SkeletonBar className="h-[14px]! w-2/3" />
          ) : values.length ? (
            // Each level is a separate value, so each line has its own copy button.
            values.map((line, index) => <CopyableValue key={`${index}-${line}`} label="Location" value={line} />)
          ) : (
            "—"
          )}
        </div>
      </div>
    </Row>
  );
};

export default LocationInfo;
