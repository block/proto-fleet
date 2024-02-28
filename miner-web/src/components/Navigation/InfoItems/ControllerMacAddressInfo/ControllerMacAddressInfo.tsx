import { useMemo } from "react";

import { getMacAddressDisplay } from "common/utils/stringUtils";

import InfoItem from "../InfoItem";

export interface ControllerMacAddressInfoProps {
  loading?: boolean;
  macAddress?: string;
}

const ControllerMacAddressInfo = ({
  loading,
  macAddress,
}: ControllerMacAddressInfoProps) => {
  const displayMacAddress = useMemo(() => getMacAddressDisplay(macAddress), [macAddress]);

  return (
    <InfoItem
      label="Controller MAC Address"
      value={displayMacAddress}
      loading={loading}
    />
  );
};

export default ControllerMacAddressInfo;
