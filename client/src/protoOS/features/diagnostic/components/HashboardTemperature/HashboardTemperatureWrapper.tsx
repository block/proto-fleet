import { useParams } from "react-router-dom";
import HashboardTemperature from "./HashboardTemperature";

const HashboardTemperatureWrapper = () => {
  const { serial } = useParams<{ serial: string }>();
  return <>{serial !== undefined ? <HashboardTemperature serial={serial} /> : null}</>;
};

export default HashboardTemperatureWrapper;
