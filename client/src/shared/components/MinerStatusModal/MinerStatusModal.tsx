import { useMemo } from "react";
import { ProtoRigIcons } from "./icons";
import MinerStatusRows from "./MinerStatusRows";

import { type MinerStatus } from "./types";
import { Alert, Checkmark, Info } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import "./style.css";

interface MinerStatusModalProps {
  status: MinerStatus;
  onDismiss: () => void;
  onWake?: () => void;
  isProtoRig?: boolean;
}

const MinerStatusModal = ({
  onDismiss,
  status,
  onWake,
  isProtoRig,
}: MinerStatusModalProps) => {
  const hasIssues = Object.values(status?.issues || {}).some(
    (issueList) => issueList.length > 0,
  );

  const icon = useMemo(() => {
    if (status.isSleeping) {
      return <Info className="text-core-primary-20" width={iconSizes.xLarge} />;
    } else if (hasIssues) {
      return <Alert className="text-text-critical" width={iconSizes.xLarge} />;
    } else
      return (
        <Checkmark
          className="rounded-full bg-intent-success-fill text-surface-base"
          width={iconSizes.xLarge}
        />
      );
  }, [hasIssues, status.isSleeping]);

  const buttons = useMemo(() => {
    const wakeBtn = {
      text: "Wake miner",
      variant: variants.secondary,
      onClick: onWake,
    };

    const doneBtn = {
      text: "Done",
      variant: variants.primary,
      onClick: onDismiss,
    };

    if (status.isSleeping) {
      return [wakeBtn, doneBtn];
    }

    return [doneBtn];
  }, [status.isSleeping, onWake, onDismiss]);

  return (
    <Modal buttons={buttons} title="Miner status" onDismiss={onDismiss}>
      <div className="space-y-6">
        <div className="mt-6 flex flex-col gap-2">
          <div>{icon}</div>
          <div className="text-heading-300 text-text-primary">
            {status.title}
          </div>
        </div>
        <div className="miner-status-tab-content-wrapper mt-0! max-h-[30vh] overflow-y-auto">
          <MinerStatusRows
            issues={status.isSleeping ? undefined : status.issues?.controlBoard}
            disabled={status.isSleeping}
            icon={ProtoRigIcons.controlBoard}
            componentName="Control board"
            isProtoRig={isProtoRig}
          />
          <MinerStatusRows
            issues={status.isSleeping ? undefined : status.issues?.fans}
            disabled={status.isSleeping}
            icon={ProtoRigIcons.fan}
            componentName="Fan"
            isProtoRig={isProtoRig}
          />
          <MinerStatusRows
            issues={status.isSleeping ? undefined : status.issues?.hashboards}
            disabled={status.isSleeping}
            icon={ProtoRigIcons.hashboard}
            componentName="Hashboard"
            isProtoRig={isProtoRig}
          />
          <MinerStatusRows
            issues={status.isSleeping ? undefined : status.issues?.psus}
            disabled={status.isSleeping}
            icon={ProtoRigIcons.psu}
            componentName="Power supply"
            isProtoRig={isProtoRig}
          />
        </div>
      </div>
    </Modal>
  );
};

export default MinerStatusModal;
