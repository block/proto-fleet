import HbTempPreview from "../HbTempPreview";
import { type HashboardData } from "@/protoOS/store";

type HbBayPreviewProps = {
  data: HashboardData[];
};

const HbBayPreview = ({ data }: HbBayPreviewProps) => {
  return (
    <div className="mb-4 flex flex-col overflow-hidden rounded-xl border-1 border-border-10 phone:grid-cols-1 phone:gap-y-4 phone:rounded-none phone:shadow-none">
      {data.map((hbData) => (
        <HbTempPreview key={hbData.serial} hbData={hbData} />
      ))}
    </div>
  );
};

export default HbBayPreview;
