import { StatusCell } from "./channelStatus";
import {
  CONTROLLER_SOFTWARE_COMPONENT,
  controllerSoftwareOptions,
  controllerSoftwareOptionValue,
  controllerSoftwareStatus,
} from "./controllerSoftwareUtils";
import FirmwarePickerButton from "./FirmwarePickerButton";
import { pairLabel } from "./rolloutStatus";
import type { ChannelControllerSoftware, ControllerSoftwarePair } from "./useChannelControllerSoftware";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";

// One container model's controller software row: the version the channel
// names, how many containers are on it, and the way to see which are not.
const ControllerSoftwareRow = ({
  group,
  state,
  disabled,
}: {
  group: ControllerSoftwarePair & { minerCount: number };
  state: ChannelControllerSoftware;
  disabled?: boolean;
}) => {
  const software = state.forPair(group);
  const target = state.targetForPair(group);
  const refreshing = state.isRefreshingPair(group);
  const options = controllerSoftwareOptions(state.versions, target);
  const status = software && !refreshing ? controllerSoftwareStatus(software) : null;

  return (
    <tr className="border-t border-border-5" data-testid={`controller-software-row-${group.model}`}>
      <td className="py-3 pr-4">
        <div className="text-emphasis-300">{pairLabel(group)}</div>
        <div className="text-text-primary-70">{CONTROLLER_SOFTWARE_COMPONENT}</div>
      </td>
      <td className="py-3 pr-4">{(software?.containerCount ?? group.minerCount).toLocaleString()}</td>
      <td className="py-3 pr-4">
        {state.error ? (
          <div className="mb-2 grid justify-items-start gap-1">
            <p role="alert" className="text-200 text-intent-critical-fill">
              {state.models === null ? state.error : `Controller software status may be out of date. ${state.error}`}
            </p>
            <Button text="Retry" variant={variants.secondary} size={buttonSizes.compact} onClick={state.retry} />
          </div>
        ) : null}
        {state.models === null ? (
          state.error ? null : (
            <span className="text-text-primary-50">Loading…</span>
          )
        ) : state.canSet ? (
          <div className="grid gap-1">
            <FirmwarePickerButton
              label={`${CONTROLLER_SOFTWARE_COMPONENT} for ${pairLabel(group)}`}
              options={options}
              value={controllerSoftwareOptionValue(options, target)}
              onChange={(version) => state.requestChange(group, version)}
              testId={`controller-software-select-${group.model}`}
              disabled={disabled || state.isSetting}
            />
            {state.versionsError ? (
              <p role="alert" className="text-200 text-intent-critical-fill">
                {state.versionsError}
              </p>
            ) : null}
          </div>
        ) : (
          <span className={target === "" ? "text-text-primary-50" : undefined}>{target || "Not set"}</span>
        )}
      </td>
      <td className="py-3 pr-4">
        {refreshing ? (
          <span className="text-text-primary-50">Refreshing status…</span>
        ) : status ? (
          <StatusCell status={status} testId={`controller-software-status-${group.model}`} />
        ) : (
          <span className="text-text-primary-50">—</span>
        )}
      </td>
      <td className="py-3 text-right">
        {software && software.containerCount > 0 ? (
          <Button
            variant={variants.secondary}
            size={buttonSizes.compact}
            text="View containers"
            ariaLabel={`View ${pairLabel(group)} controller software by container`}
            onClick={() => state.viewContainers(group)}
            testId={`view-controller-software-${group.model}`}
          />
        ) : null}
      </td>
    </tr>
  );
};

export default ControllerSoftwareRow;
