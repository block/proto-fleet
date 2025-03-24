import { Link } from "react-router-dom";
import { useMinerHosting } from "@/protoOS/api";

const HashboardTemperature = () => {
  // const { serial } = useParams<{ serial: string }>();
  const { minerRoot } = useMinerHosting();
  return (
    <div className="flex min-h-[100vh] w-full flex-col items-center justify-center gap-4 bg-surface-base">
      <h2 className="text-lg font-semibold">Under Construction 🚧</h2>
      <Link to={minerRoot + `/temperature`}>Close</Link>
    </div>
  );
};

export default HashboardTemperature;
