import { useMemo } from "react";
import { useShallow } from "zustand/shallow";

import HbTempPreview from "./HbTempPreview";
import { useHashboardStats } from "@/protoOS/api";
import { sortAsics } from "@/protoOS/features/kpis/components/Temperature/utility";
import { HbTemperature } from "@/protoOS/features/kpis/hooks";
import useHashboardAsicStore from "@/protoOS/store/useHashboardAsicStore";

type HbTempPreviewWrapperProps = {
  hbData: HbTemperature;
};

const HbTempPreviewWrapper = ({ hbData }: HbTempPreviewWrapperProps) => {
  useHashboardStats({
    hashboardSerialNumber: hbData.serial,
    poll: true,
  });
  const { hashboard, maxAsicTempC, avgAsicTempC } = useHashboardAsicStore(
    useShallow((state) => {
      const hashboard = state.hashboards.get(hbData.serial);
      return {
        hashboard,
        maxAsicTempC: hashboard?.maxAsicTempC,
        avgAsicTempC: hashboard?.avgAsicTempC,
      };
    }),
  );

  const asics = useMemo(() => {
    if (!hashboard) return undefined;
    const asicArray = Array.from(hashboard.asics.values());
    return asicArray.length > 0 ? sortAsics(asicArray) : undefined;
  }, [hashboard]);

  return (
    <HbTempPreview
      hbData={hbData}
      asics={asics}
      avgAsicTempC={avgAsicTempC}
      maxAsicTempC={maxAsicTempC}
    />
  );
};

export default HbTempPreviewWrapper;
