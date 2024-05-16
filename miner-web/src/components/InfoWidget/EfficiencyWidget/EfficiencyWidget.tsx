import { useEffect, useMemo, useState } from "react";

import { useWindowDimensions } from "common/hooks/useWindowDimensions";
import { getDisplayValue } from "common/utils/stringUtils";
import { getRandomInt } from "common/utils/utility";

import InfoWidget from "components/InfoWidget";
import Line from "components/InfoWidget/Line";

import EfficiencyModal from "./EfficiencyModal";

interface EfficiencyWidgetProps {
  efficiency?: string | number | null;
  efficiencyValues?: Record<string, number>[];
  loading?: boolean;
}

const EfficiencyWidget = ({
  efficiency,
  efficiencyValues,
  loading,
}: EfficiencyWidgetProps) => {
  const [showModal, setShowModal] = useState(false);
  const { isDesktop } = useWindowDimensions();
  // TODO: get efficiency values from API once implemented
  const initValue = isDesktop ? efficiencyValues : efficiencyValues?.slice(2);
  const [data, setData] = useState(initValue || []);

  useEffect(() => {
    if (loading || !initValue?.length) {
      if (data.length) setData([]);
      return;
    } else if (initValue.length !== data.length) {
      setData(initValue);
      return;
    }

    let timeoutId = setTimeout(() => {
      const newData = [...data];
      newData.shift();
      newData.push({ value: getRandomInt(20, 30) });
      setData(newData);
    }, 5000);

    return () => {
      clearTimeout(timeoutId);
    };
  }, [data, initValue, loading]);

  const displayEfficiency = useMemo(
    () => efficiency && `${getDisplayValue(efficiency)} J/TH`,
    [efficiency]
  );

  return (
    <>
      <InfoWidget
        title="Efficiency"
        value={displayEfficiency}
        loading={loading}
        hasBorder
        stats={<Line data={data} />}
        onClick={loading ? undefined : () => setShowModal(true)}
        className="z-10"
      />
      {showModal && (
        <EfficiencyModal
          onDismiss={() => setShowModal(false)}
          efficiency={displayEfficiency}
        />
      )}
    </>
  );
};

export default EfficiencyWidget;
