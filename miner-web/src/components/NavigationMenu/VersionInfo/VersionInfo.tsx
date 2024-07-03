import Row from "components/Row";
import SkeletonBar from "components/SkeletonBar";

export interface VersionInfoProps {
  loading?: boolean;
  value?: string;
}

const VersionInfo = ({ loading, value }: VersionInfoProps) => {
  return (
    <div className="-mb-2">
      <Row
        compact
        divider={false}
        className="flex items-center"
        testId="version-info-item"
      >
        <div className="grow">
          <div className="relative text-200 text-text-contrast/70">
            Firmware Version
          </div>
          <div className="font-mono text-mono-text-50 text-text-contrast/70">
            {loading ? (
              <SkeletonBar className="w-4/5" theme="dark" />
            ) : (
              value ?? "-"
            )}
          </div>
        </div>
      </Row>
    </div>
  );
};

export default VersionInfo;
