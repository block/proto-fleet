import { useMemo } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import HashboardTemperature from "./HashboardTemperature";
import { useHashboardStatus } from "@/protoOS/api/hooks/useHashboardStatus";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { findModuleInTankByRouteParam, findTankByRouteParam } from "@/protoOS/topologyRouteParams";
import Button from "@/shared/components/Button";

const HashboardTemperatureWrapper = () => {
  const { serial, tankId, moduleId } = useParams();
  const navigate = useNavigate();
  const { search } = useLocation();
  const topology = useMinerStore((state) => state.hardware.topology);
  const tank = findTankByRouteParam(topology, tankId);
  const module = topology && tank ? findModuleInTankByRouteParam(topology, tank, moduleId) : undefined;
  const valid =
    !moduleId || !!module?.hashboardIds.some((id) => topology?.hashboards.get(id)?.identity.value === serial);
  const hashboardSerialNumbers = useMemo(() => (valid && serial !== undefined ? [serial] : []), [serial, valid]);
  const { error } = useHashboardStatus({ hashboardSerialNumbers, poll: topology?.kind === "container" });
  if (moduleId && !topology) return <p role="status">Loading module…</p>;
  if (!valid)
    return (
      <div className="flex flex-col items-start gap-4 p-6">
        <p role="status">Hashboard not found in this module.</p>
        <Button
          variant="secondary"
          text="Back to module"
          onClick={() => navigate({ pathname: "..", search }, { relative: "path" })}
        />
      </div>
    );
  return serial !== undefined ? (
    <>
      {error ? <p role="alert">Could not refresh ASIC readings.</p> : null}
      <HashboardTemperature serial={serial} />
    </>
  ) : null;
};

export default HashboardTemperatureWrapper;
