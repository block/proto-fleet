import { useMemo } from "react";

import InfoItem from "../InfoItem";
import { getMacAddressDisplay } from "@/shared/utils/stringUtils";

export interface MacAddressInfoProps {
  loading?: boolean;
  value?: string;
}

const MacAddressInfo = ({ loading, value }: MacAddressInfoProps) => {
  const displayValue = useMemo(() => getMacAddressDisplay(value), [value]);

  return (
    <InfoItem
      label="MAC Address"
      loading={loading}
      value={displayValue}
      divider={false}
      testId="mac-address-info-item"
    />
  );
};

export default MacAddressInfo;
