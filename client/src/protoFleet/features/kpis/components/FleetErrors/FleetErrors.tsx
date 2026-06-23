import ComponentErrors from "../ComponentErrors";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { type ActiveSite, DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
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
  activeSite?: ActiveSite;
};

const FleetErrors = ({
  controlBoardErrors,
  fanErrors,
  hashboardErrors,
  psuErrors,
  className,
  extraFilterParams,
  activeSite = DEFAULT_ACTIVE_SITE,
}: FleetErrorsProps) => {
  const suffix = extraFilterParams ? `&${extraFilterParams}` : "";
  const minerIssuesHref = (issue: string) => scopedPath(`/fleet/miners?issues=${issue}${suffix}`, activeSite);
  return (
    <div className={className}>
      <div className="grid grid-cols-1 gap-1 tablet:grid-cols-2 laptop:grid-cols-4">
        <ComponentErrors
          icon={<ControlBoard />}
          heading="Control Boards"
          errorCount={controlBoardErrors}
          href={minerIssuesHref("control-board")}
        />
        <ComponentErrors icon={<Fan />} heading="Fans" errorCount={fanErrors} href={minerIssuesHref("fans")} />
        <ComponentErrors
          icon={<Hashboard />}
          heading="Hashboards"
          errorCount={hashboardErrors}
          href={minerIssuesHref("hash-boards")}
        />
        <ComponentErrors
          icon={<LightningAlt />}
          heading="Power supplies"
          errorCount={psuErrors}
          href={minerIssuesHref("psu")}
        />
      </div>
    </div>
  );
};

export default FleetErrors;
