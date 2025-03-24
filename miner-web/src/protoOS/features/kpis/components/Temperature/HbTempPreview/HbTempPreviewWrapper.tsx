import { useEffect, useState } from "react";
import { HbTemperature } from "../../../hooks";
import { sortAsics } from "../utility";
import HbTempPreview from "./HbTempPreview";
import { useHashboardStats } from "@/protoOS/api";
import { AsicStats } from "@/protoOS/api/types";

type HbTempPreviewWrapperProps = {
  hbData: HbTemperature;
};

const HbTempPreviewWrapper = ({ hbData }: HbTempPreviewWrapperProps) => {
  const [asics, setAsics] = useState<AsicStats[] | undefined>();
  const { data, pending } = useHashboardStats({
    hashboardSerialNumber: hbData.serial,
    poll: true,
  });

  useEffect(() => {
    if (!pending && data?.asics?.length) {
      setAsics(sortAsics(data.asics));
    }
  }, [data, pending]);

  return <HbTempPreview hbData={hbData} asics={asics} />;
};

export default HbTempPreviewWrapper;
