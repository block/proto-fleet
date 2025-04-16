import { useMemo } from "react";

import MinerStatusRow from "./MinerStatusRow";
import MinerStatusRows from "./MinerStatusRows";
import {
  getErrorTitle,
  isAsicError,
  isAsicWarning,
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
  const asicErrors = useMemo(() => errors.filter(isAsicError), [errors]);
  const asicWarnings = useMemo(() => errors.filter(isAsicWarning), [errors]);
  const fanErrors = useMemo(() => errors.filter(isFanError), [errors]);
  const fanWarnings = useMemo(() => errors.filter(isFanWarning), [errors]);

  const errorCount = useMemo(
    () => hashboardErrors.length + asicErrors.length + fanErrors.length,
    [hashboardErrors, asicErrors, fanErrors],
  );

  const warningCount = useMemo(
    () => hashboardWarnings.length + asicWarnings.length + fanWarnings.length,
    [hashboardWarnings, asicWarnings, fanWarnings],
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
      buttons={[
        {
          text: "Contact support",
          variant: variants.secondary,
          onClick: () => {
            window.open("mailto:mining.support@block.xyz", "_blank");
          },
        },
        {
          text: "Done",
          variant: variants.primary,
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
                <MinerStatusRows errors={hashboardErrors} />
                <MinerStatusRows errors={hashboardWarnings} />
                <MinerStatusRows errors={fanErrors} />
                <MinerStatusRows errors={fanWarnings} />
                <MinerStatusRows errors={psuErrors} />
                <MinerStatusRows errors={psuWarnings} />
                {/* <MinerStatusRows errors={asicErrors} />
                <MinerStatusRows errors={asicWarnings} /> */}
              </Tabs.Tab>
              <Tabs.Tab
                label={`${errorCount} ${errorCount === 1 ? "error" : "errors"}`}
                className="mt-0!"
              >
                {errorCount ? (
                  <>
                    <MinerStatusRows errors={hashboardErrors} />
                    <MinerStatusRows errors={fanErrors} />
                    <MinerStatusRows errors={psuErrors} />
                    {/* <MinerStatusRows errors={asicErrors} /> */}
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
                    <MinerStatusRows errors={hashboardWarnings} />
                    <MinerStatusRows errors={fanWarnings} />
                    <MinerStatusRows errors={psuErrors} />
                    {/* <MinerStatusRows errors={asicWarnings} /> */}
                  </>
                ) : (
                  <div className="mt-3">No warnings</div>
                )}
              </Tabs.Tab>
            </Tabs>
          ) : (
            <>
              <Divider />
              <MinerStatusRow label="Hashboards" />
              <MinerStatusRow label="Fans" />
              <MinerStatusRow label="PSU" />
              {/* <MinerStatusRow label="ASICs" /> */}
            </>
          )}
        </div>
      </div>
    </Modal>
  );
};

export default MinerStatusModal;
