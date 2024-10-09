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
    <InfoItem
      label="Mac Address"
      loading={loading}
      value={displayValue}
      testId="mac-address-info-item"
    />
  );
};

export default MacAddressInfo;
