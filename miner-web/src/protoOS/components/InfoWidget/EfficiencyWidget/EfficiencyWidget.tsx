import { useEffect, useState } from "react";

import EfficiencyModal from "./EfficiencyModal";
import InfoWidget from "@/protoOS/components/InfoWidget";
import Line from "@/protoOS/components/InfoWidget/Line";
import { Duration } from "@/shared/components/DurationSelector";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

import { getDisplayValue } from "@/shared/utils/stringUtils";

interface EfficiencyWidgetProps {
  avgEfficiency?: string | number | null;
  efficiencyValues?: Record<string, number | string>[];
  duration: Duration;
  loading?: boolean;
}

const EfficiencyWidget = ({
  avgEfficiency,
  efficiencyValues,
  duration,
  loading,
}: EfficiencyWidgetProps) => {
  const [efficiency, setEfficiency] = useState<string | number>();
  const [showModal, setShowModal] = useState(false);
  const { isDesktop } = useWindowDimensions();
  const data = isDesktop
    ? efficiencyValues?.slice(-5)
    : efficiencyValues?.slice(-2);

  useEffect(() => {
    setEfficiency(
      getDisplayValue(efficiencyValues?.[efficiencyValues.length - 1]?.value)
    );
  }, [efficiencyValues]);

  return (
    <>
      <InfoWidget
        title="Current efficiency"
        value={efficiency && `${efficiency} J/TH`}
        loading={loading}
        hasBorder
        stats={!loading && data && <Line data={data} />}
        onClick={loading ? undefined : () => setShowModal(true)}
        className="z-10"
      />
      {showModal && (
        <EfficiencyModal
          efficiency={efficiency}
          avgEfficiency={avgEfficiency}
          efficiencyValues={efficiencyValues}
          duration={duration}
          onDismiss={() => setShowModal(false)}
        />
      )}
    </>
  );
};

export default EfficiencyWidget;
