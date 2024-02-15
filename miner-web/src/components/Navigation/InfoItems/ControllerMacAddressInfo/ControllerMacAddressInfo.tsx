import { useMemo } from "react";

import { getMacAddressDisplay } from "common/utils/stringUtils";

import InfoItem from "../InfoItem";

export interface ControllerMacAddressInfoProps {
  loading?: boolean;
  mac_address?: string;
}

const ControllerMacAddressInfo = ({
  loading,
  mac_address,
}: ControllerMacAddressInfoProps) => {
  const displayMacAddress = useMemo(() => getMacAddressDisplay(mac_address), [mac_address]);

  return (
    <InfoItem
      label="Controller MAC Address"
      value={displayMacAddress}
      loading={loading}
    />
  );
};

export default ControllerMacAddressInfo;
