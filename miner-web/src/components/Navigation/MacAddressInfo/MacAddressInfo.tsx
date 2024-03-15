import { useMemo } from "react";

import { getMacAddressDisplay } from "common/utils/stringUtils";

import Row from "components/Row";
import SkeletonBar from "components/SkeletonBar";

export interface MacAddressInfoProps {
  loading?: boolean;
  value?: string;
}

const MacAddressInfo = ({ loading, value }: MacAddressInfoProps) => {
  const displayValue = useMemo(() => getMacAddressDisplay(value), [value]);

  return (
    <div className="-mb-2">
      <Row
        compact
        divider={false}
        className="flex items-center"
        testId="mac-address-info-item"
      >
        <div className="grow">
          <div className="relative text-200 text-text-contrast/70">
            Mac Address
          </div>
          <div className="font-mono text-mono-text-50 text-text-contrast/70">
            {loading ? (
              <SkeletonBar className="w-4/5" theme="dark" />
            ) : (
              displayValue ?? "-"
            )}
          </div>
        </div>
      </Row>
    </div>
  );
};

export default MacAddressInfo;
