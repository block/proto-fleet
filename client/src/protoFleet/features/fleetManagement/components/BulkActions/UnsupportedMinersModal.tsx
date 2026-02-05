import { Fragment } from "react";
import { UnsupportedMinerGroup } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { Fleet } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";
import Divider from "@/shared/components/Divider";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";

interface UnsupportedMinersModalProps {
  show: boolean;
  actionDescription: string;
  unsupportedGroups: UnsupportedMinerGroup[];
  totalUnsupportedCount: number;
  /** When true, indicates no miners support this action (show "Action not supported" UI) */
  noneSupported: boolean;
  onContinue: () => void;
  onDismiss: () => void;
}

const UnsupportedMinersModal = ({
  show,
  actionDescription,
  unsupportedGroups,
  totalUnsupportedCount,
  noneSupported,
  onContinue,
  onDismiss,
}: UnsupportedMinersModalProps) => {
  if (!show) return null;

  // When all miners are unsupported, show "Action not supported" dialog with only Dismiss button
  if (noneSupported) {
    const minerText = totalUnsupportedCount === 1 ? "miner's" : "miners'";
    return (
      <Dialog
        show={show}
        title="Action not supported"
        subtitle={`This action isn't supported by the connected ${minerText} firmware.`}
        subtitleSize="text-300"
        buttonGroupVariant={groupVariants.leftAligned}
        buttons={[
          {
            text: "Dismiss",
            variant: variants.secondary,
            onClick: onDismiss,
            testId: "dismiss-button",
          },
        ]}
        testId="action-not-supported-dialog"
      />
    );
  }

  // When some miners are unsupported, show grouped list with Continue button
  return (
    <Modal
      show={show}
      title="Some miners don't support this action."
      description={`${actionDescription} will be skipped for ${totalUnsupportedCount} miners.`}
      onDismiss={onDismiss}
      buttons={[
        {
          text: "Continue",
          variant: variants.primary,
          onClick: onContinue,
          dismissModalOnClick: false,
          testId: "continue-button",
        },
      ]}
      size="small"
      divider={false}
    >
      <div className="mt-4 rounded-2xl border border-core-primary-5 p-4">
        {unsupportedGroups.map((group, index) => (
          <Fragment key={`${group.firmwareVersion}-${group.model}`}>
            <Row divider={false} className="flex items-center justify-between">
              <div className="flex gap-4">
                <Fleet width="w-[20px]" />
                <div>
                  <div className="text-emphasis-300">Firmware {group.firmwareVersion}</div>
                  <div className="text-200 text-text-primary-70">{group.model}</div>
                </div>
              </div>
              <div className="text-emphasis-300">
                {group.count} {group.count === 1 ? "miner" : "miners"}
              </div>
            </Row>
            {index < unsupportedGroups.length - 1 && <Divider />}
          </Fragment>
        ))}
      </div>
    </Modal>
  );
};

export default UnsupportedMinersModal;
