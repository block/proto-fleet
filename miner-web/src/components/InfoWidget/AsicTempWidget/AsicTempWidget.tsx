import { useMemo, useState } from "react";

import { getDisplayValue } from "common/utils/stringUtils";

import InfoWidget, { Bar } from "components/InfoWidget";

import { getIntensity } from "../utility";
import AsicTempModal from "./AsicTempModal";

interface AsicTempWidgetProps {
  asicTemp?: string | number;
  loading?: boolean;
}

const AsicTempWidget = ({ asicTemp, loading }: AsicTempWidgetProps) => {
  const [showModal, setShowModal] = useState(false);
  // TODO: calculate intensity based on the actual data when API returns max value
  const max = 100;

  const intensity = useMemo(() => getIntensity(asicTemp, max), [asicTemp]);

  // \u00B0c is the degree symbol
  const displayAsicTemp = useMemo(
    () => asicTemp && `${getDisplayValue(asicTemp)}\u00B0c`,
    [asicTemp]
  );

  return (
    <>
      <InfoWidget
        title="Avg. ASIC temp"
        value={displayAsicTemp}
        loading={loading}
        hasBorder
        stats={<Bar intensity={loading ? 0 : intensity} />}
        onClick={loading ? undefined : () => setShowModal(true)}
      />
      {showModal && (
        <AsicTempModal
          onDismiss={() => setShowModal(false)}
          avgAsicTemp={displayAsicTemp}
        />
      )}
    </>
  );
};

export default AsicTempWidget;
