import { useMemo } from "react";

import { getMacAddressDisplay } from "common/utils/stringUtils";

import InfoItem from "../InfoItem";

export interface MacAddressInfoProps {
  loading?: boolean;
  value?: string;
}

const MacAddressInfo = ({ loading, value }: MacAddressInfoProps) => {
  const displayValue = useMemo(() => getMacAddressDisplay(value), [value]);

  return (
    <div className="-mb-2">
      <InfoItem
        label="Mac Address"
        value={displayValue}
        loading={loading}
        testId="mac-address-info-item"
      />
    </div>
  );
};

export default MacAddressInfo;
