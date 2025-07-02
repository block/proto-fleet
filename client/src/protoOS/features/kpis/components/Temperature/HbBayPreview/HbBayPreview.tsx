import HbTempPreview from "../HbTempPreview";
import { type HbTemperature } from "@/protoOS/features/kpis/hooks";

type HbBayPreviewProps = {
  data: HbTemperature[];
};

const HbBayPreview = ({ data }: HbBayPreviewProps) => {
  return (
    <div className="mb-4 flex flex-col overflow-hidden rounded-xl border-1 border-border-10 phone:grid-cols-1 phone:gap-y-4 phone:rounded-none phone:shadow-none">
      {data.map((hbData, index) => (
        <HbTempPreview key={index} hbData={hbData} />
      ))}
    </div>
  );
};

export default HbBayPreview;
