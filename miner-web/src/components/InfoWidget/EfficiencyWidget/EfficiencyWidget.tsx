import { useState } from "react";

import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import InfoWidget from "components/InfoWidget";
import Line from "components/InfoWidget/Line";

import EfficiencyModal from "./EfficiencyModal";

interface EfficiencyWidgetProps {
  avgEfficiency?: string | number | null;
  efficiency?: string | number | null;
  efficiencyValues?: Record<string, number | string>[];
  loading?: boolean;
}

const EfficiencyWidget = ({
  avgEfficiency,
  efficiency,
  efficiencyValues,
  loading,
}: EfficiencyWidgetProps) => {
  const [showModal, setShowModal] = useState(false);
  const { isDesktop } = useWindowDimensions();
  const data = isDesktop
    ? efficiencyValues?.slice(-5)
    : efficiencyValues?.slice(-2);

  return (
    <>
      <InfoWidget
        title="Efficiency"
        value={efficiency && `${efficiency} J/TH`}
        loading={loading}
        hasBorder
        stats={data && <Line data={data} />}
        onClick={loading ? undefined : () => setShowModal(true)}
        className="z-10"
      />
      {showModal && (
        <EfficiencyModal
          efficiency={efficiency}
          avgEfficiency={avgEfficiency}
          efficiencyValues={efficiencyValues}
          onDismiss={() => setShowModal(false)}
        />
      )}
    </>
  );
};

export default EfficiencyWidget;
