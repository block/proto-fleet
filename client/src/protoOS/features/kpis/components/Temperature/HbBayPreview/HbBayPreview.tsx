import HbTempPreview from "../HbTempPreview";
import { useSlotsPerBay } from "@/protoOS/store";

type HbBayPreviewProps = {
  serials: (string | null)[];
  bay: number;
};

const HbBayPreview = ({ serials, bay }: HbBayPreviewProps) => {
  const slotsPerBay = useSlotsPerBay();
  return (
    <div className="mb-4 flex flex-col overflow-hidden rounded-xl border-1 border-border-10 phone:grid-cols-1 phone:gap-y-4 phone:rounded-none phone:shadow-none">
      {serials.map((serial, idx) => (
        <HbTempPreview
          key={`${bay}-${idx}`}
          serial={serial}
          slot={bay * slotsPerBay + idx + 1}
        />
      ))}
    </div>
  );
};

export default HbBayPreview;
