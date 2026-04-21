import ComponentErrors from "../ComponentErrors";
import ControlBoard from "@/shared/assets/icons/ControlBoard";
import Fan from "@/shared/assets/icons/Fan";
import Hashboard from "@/shared/assets/icons/Hashboard";
import LightningAlt from "@/shared/assets/icons/LightningAlt";

type FleetErrorsProps = {
  controlBoardErrors?: number;
  fanErrors?: number;
  hashboardErrors?: number;
  psuErrors?: number;
  className?: string;
  extraFilterParams?: string;
};

const FleetErrors = ({
  controlBoardErrors,
  fanErrors,
  hashboardErrors,
  psuErrors,
  className,
  extraFilterParams,
}: FleetErrorsProps) => {
  const suffix = extraFilterParams ? `&${extraFilterParams}` : "";
  return (
    <div className={className}>
      <div className="grid grid-cols-4 gap-1 phone:grid-cols-1 tablet:grid-cols-2">
        <ComponentErrors
          icon={<ControlBoard />}
          heading="Control Boards"
          errorCount={controlBoardErrors}
          href={`/miners?issues=control-board${suffix}`}
        />
        <ComponentErrors icon={<Fan />} heading="Fans" errorCount={fanErrors} href={`/miners?issues=fans${suffix}`} />
        <ComponentErrors
          icon={<Hashboard />}
          heading="Hashboards"
          errorCount={hashboardErrors}
          href={`/miners?issues=hash-boards${suffix}`}
        />
        <ComponentErrors
          icon={<LightningAlt />}
          heading="Power supplies"
          errorCount={psuErrors}
          href={`/miners?issues=psu${suffix}`}
        />
      </div>
    </div>
  );
};

export default FleetErrors;
