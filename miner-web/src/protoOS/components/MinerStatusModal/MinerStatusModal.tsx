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
        className="bg-intent-success-fill rounded-full text-surface-base"
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
        <div>
          <div>{icon}</div>
          <div className="text-text-primary text-heading-300 mt-2">{title}</div>
        </div>
        <div>
          {hasErrors || hasWarnings ? (
            <Tabs disableAnimation>
              <Tabs.Tab
                label="All"
                className="mt-0! miner-status-tab-content-wrapper"
              >
                <MinerStatusRows errors={hashboardErrors} />
                <MinerStatusRows errors={hashboardWarnings} />
                <MinerStatusRows errors={asicErrors} />
                <MinerStatusRows errors={asicWarnings} />
                <MinerStatusRows errors={fanErrors} />
                <MinerStatusRows errors={fanWarnings} />
              </Tabs.Tab>
              <Tabs.Tab
                label={`${errorCount} ${errorCount === 1 ? "error" : "errors"}`}
                className="mt-0!"
              >
                {errorCount ? (
                  <>
                    <MinerStatusRows errors={hashboardErrors} />
                    <MinerStatusRows errors={asicErrors} />
                    <MinerStatusRows errors={fanErrors} />
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
                    <MinerStatusRows errors={asicWarnings} />
                    <MinerStatusRows errors={fanWarnings} />
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
              <MinerStatusRow label="ASICs" />
              <MinerStatusRow label="Fans" />
            </>
          )}
        </div>
      </div>
    </Modal>
  );
};

export default MinerStatusModal;
