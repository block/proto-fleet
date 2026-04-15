import { Fragment } from "react";
import { UnsupportedMinerGroup } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { Fleet } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";
import Divider from "@/shared/components/Divider";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";

interface UnsupportedMinersModalProps {
  open?: boolean;
  unsupportedGroups: UnsupportedMinerGroup[];
  totalUnsupportedCount: number;
  noneSupported: boolean;
  onContinue: () => void;
  onDismiss: () => void;
}

const UnsupportedMinersModal = ({
  open,
  unsupportedGroups,
  totalUnsupportedCount,
  noneSupported,
  onContinue,
  onDismiss,
}: UnsupportedMinersModalProps) => {
  const minerText = totalUnsupportedCount === 1 ? "miner's" : "miners'";

  return (
    <>
      <Dialog
        open={open && noneSupported}
        title="Action not supported"
        subtitle={`This action is not supported by the connected ${minerText} firmware.`}
        subtitleSize="text-300"
        buttonGroupVariant={groupVariants.leftAligned}
        onDismiss={onDismiss}
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
      <Modal
        open={open && !noneSupported}
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
        divider={false}
      >
        <div className="mb-6">
          <h1 className="text-heading-300 text-text-primary">Some miners do not support this action.</h1>
          <p className="text-300 text-text-primary-70">
            This action will be skipped for {totalUnsupportedCount} miners.
          </p>
        </div>
        {unsupportedGroups.map((group, index) => (
          <Fragment key={`${group.firmwareVersion}-${group.model}`}>
            <Row divider={false} className="flex items-center justify-between">
              <div className="flex gap-4">
                <Fleet width={iconSizes.medium} />
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
      </Modal>
    </>
  );
};

export default UnsupportedMinersModal;
