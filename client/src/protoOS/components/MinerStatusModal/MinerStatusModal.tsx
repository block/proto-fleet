import { useMemo } from "react";
import { R2_ICONS } from "./icons";
import MinerStatusRow from "./MinerStatusRow";
import MinerStatusRows from "./MinerStatusRows";
import {
  getErrorTitle,
  isControlBoardError,
  isControlBoardWarning,
  isFanError,
  isFanWarning,
  isHashboardError,
  isHashboardWarning,
  isPSUError,
  isPSUWarning,
} from "./utility";
import { ErrorListResponse } from "@/protoOS/api/types";

import { Alert, Checkmark, Stop } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

import { variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Modal from "@/shared/components/Modal";
import Tabs from "@/shared/components/Tab";
import "./style.css";

interface MinerStatusModalProps {
  errors?: ErrorListResponse;
  onDismiss: () => void;
}

const MinerStatusModal = ({
  errors = [],
  onDismiss,
}: MinerStatusModalProps) => {
  const hashboardErrors = useMemo(
    () => errors.filter(isHashboardError),
    [errors],
  );
  const hashboardWarnings = useMemo(
    () => errors.filter(isHashboardWarning),
    [errors],
  );
  const psuErrors = useMemo(() => errors.filter(isPSUError), [errors]);
  const psuWarnings = useMemo(() => errors.filter(isPSUWarning), [errors]);
  const fanErrors = useMemo(() => errors.filter(isFanError), [errors]);
  const fanWarnings = useMemo(() => errors.filter(isFanWarning), [errors]);
  const controlBoardErrors = useMemo(
    () => errors.filter(isControlBoardError),
    [errors],
  );
  const controlBoardWarnings = useMemo(
    () => errors.filter(isControlBoardWarning),
    [errors],
  );

  const errorCount = useMemo(
    () =>
      hashboardErrors.length +
      psuErrors.length +
      fanErrors.length +
      controlBoardErrors.length,
    [hashboardErrors, psuErrors, fanErrors, controlBoardErrors],
  );

  const warningCount = useMemo(
    () =>
      hashboardWarnings.length +
      psuWarnings.length +
      fanWarnings.length +
      controlBoardWarnings.length,
    [hashboardWarnings, psuWarnings, fanWarnings, controlBoardWarnings],
  );

  const hasErrors = useMemo(() => errorCount > 0, [errorCount]);

  const hasWarnings = useMemo(() => warningCount > 0, [warningCount]);

  const icon = useMemo(() => {
    if (hasErrors) {
      return <Stop className="text-text-critical" width={iconSizes.xLarge} />;
    }
    if (hasWarnings) {
      return <Alert className="text-text-warning" width={iconSizes.xLarge} />;
    }
    return (
      <Checkmark
        className="rounded-full bg-intent-success-fill text-surface-base"
        width={iconSizes.xLarge}
      />
    );
  }, [hasErrors, hasWarnings]);

  const title = useMemo(() => {
    if (hasErrors || hasWarnings) {
      return getErrorTitle(errors);
    }
    return "All systems are operational";
  }, [hasErrors, hasWarnings, errors]);

  return (
    <Modal
      className="phone:w-[calc(100vw-theme(spacing.4))] tablet:w-[calc(100vw-theme(spacing.4))]"
      buttons={[
        {
          text: "Done",
          variant: variants.primary,
          onClick: onDismiss,
        },
      ]}
      title="Miner status"
      onDismiss={onDismiss}
    >
      <div className="space-y-6">
        <div className="mt-6 flex flex-col gap-2">
          <div>{icon}</div>
          <div className="text-heading-300 text-text-primary">{title}</div>
        </div>
        <div>
          {hasErrors || hasWarnings ? (
            <Tabs disableAnimation>
              <Tabs.Tab
                label="All"
                className="miner-status-tab-content-wrapper mt-0!"
              >
                <MinerStatusRows errors={fanErrors} icon={R2_ICONS.fan} />
                <MinerStatusRows errors={fanWarnings} icon={R2_ICONS.fan} />
                <MinerStatusRows
                  errors={hashboardErrors}
                  icon={R2_ICONS.hashboard}
                />
                <MinerStatusRows
                  errors={hashboardWarnings}
                  icon={R2_ICONS.hashboard}
                />
                <MinerStatusRows
                  errors={controlBoardErrors}
                  icon={R2_ICONS.controlBoard}
                />
                <MinerStatusRows
                  errors={controlBoardWarnings}
                  icon={R2_ICONS.controlBoard}
                />
                <MinerStatusRows errors={psuErrors} icon={R2_ICONS.psu} />
                <MinerStatusRows errors={psuWarnings} icon={R2_ICONS.psu} />
              </Tabs.Tab>
              <Tabs.Tab
                label={`${errorCount} ${errorCount === 1 ? "error" : "errors"}`}
                className="mt-0!"
              >
                {errorCount ? (
                  <>
                    <MinerStatusRows errors={fanErrors} icon={R2_ICONS.fan} />
                    <MinerStatusRows
                      errors={hashboardErrors}
                      icon={R2_ICONS.hashboard}
                    />
                    <MinerStatusRows
                      errors={controlBoardErrors}
                      icon={R2_ICONS.controlBoard}
                    />
                    <MinerStatusRows errors={psuErrors} icon={R2_ICONS.psu} />
                  </>
                ) : (
                  <div className="mt-3">No errors</div>
                )}
              </Tabs.Tab>
              <Tabs.Tab
                label={`${warningCount} ${warningCount === 1 ? "warning" : "warnings"}`}
                className="mt-0!"
              >
                {warningCount ? (
                  <>
                    <MinerStatusRows errors={fanWarnings} icon={R2_ICONS.fan} />
                    <MinerStatusRows
                      errors={hashboardWarnings}
                      icon={R2_ICONS.hashboard}
                    />
                    <MinerStatusRows
                      errors={controlBoardWarnings}
                      icon={R2_ICONS.controlBoard}
                    />
                    <MinerStatusRows errors={psuWarnings} icon={R2_ICONS.psu} />
                  </>
                ) : (
                  <div className="mt-3">No warnings</div>
                )}
              </Tabs.Tab>
            </Tabs>
          ) : (
            <>
              <Divider />
              <MinerStatusRow label="Fans" icon={R2_ICONS.fan} />
              <MinerStatusRow label="Hashboards" icon={R2_ICONS.hashboard} />
              <MinerStatusRow
                label="Control board"
                icon={R2_ICONS.controlBoard}
              />
              <MinerStatusRow label="PSU" icon={R2_ICONS.psu} />
            </>
          )}
        </div>
      </div>
    </Modal>
  );
};

export default MinerStatusModal;
