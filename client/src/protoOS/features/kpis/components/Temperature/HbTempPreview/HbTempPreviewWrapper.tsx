import { useShallow } from "zustand/shallow";

import { HbTemperature } from "@/protoOS/features/kpis/hooks";
import useHashboardAsicStore from "@/protoOS/store/useHashboardAsicStore";
import { AsicData } from "@/protoOS/store/useHashboardAsicStore";
import { sortAsics } from "@/protoOS/features/kpis/components/Temperature/utility";
import HbTempPreview from "./HbTempPreview";
import { useHashboardStats } from "@/protoOS/api";

type HbTempPreviewWrapperProps = {
  hbData: HbTemperature;
};

const HbTempPreviewWrapper = ({ hbData }: HbTempPreviewWrapperProps) => {
  useHashboardStats({
    hashboardSerialNumber: hbData.serial,
    poll: true,
  });
  const asics = useHashboardAsicStore(
    useShallow((state) => {
      const hashboard = state.hashboards.get(hbData.serial);
      const asicArray: AsicData[] = hashboard
        ? Array.from(hashboard.asics.values())
        : [];

      return asicArray.length > 0 ? sortAsics(asicArray) : undefined;
    }),
  );

  return <HbTempPreview hbData={hbData} asics={asics} />;
};

export default HbTempPreviewWrapper;
