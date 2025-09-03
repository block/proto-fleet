import { useEffect, useMemo } from "react";
import { useFirmwareUpdate } from "@/protoOS/api";
import {
  statuses,
  useFirmwareUpdateContext,
} from "@/protoOS/features/firmwareUpdate/contexts/FirmwareUpdateContext";

import { sizes, variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

type InfoModalProps = {
  closeModal: () => void;
};

const InfoModal = ({ closeModal }: InfoModalProps) => {
  const { updateFirmware } = useFirmwareUpdate();
  const { updateStatus, pending, installing } = useFirmwareUpdateContext();

  const installButton = useMemo(
    () => ({
      text: "Install",
      variant: variants.primary,
      disabled: installing || pending,
      prefixIcon:
        installing || pending ? (
          <ProgressCircular size={16} indeterminate />
        ) : undefined,
      dismissModalOnClick: false,
      onClick: updateFirmware,
    }),
    [installing, pending, updateFirmware],
  );

  useEffect(() => {
    if (
      updateStatus?.status !== statuses.current &&
      updateStatus?.status !== statuses.available
    ) {
      closeModal();
    }
  }, [updateStatus, closeModal]);

  return (
    <Modal
      divider={false}
      buttons={[installButton]}
      buttonSize={sizes.base}
      onDismiss={closeModal}
      size="small"
    >
      <h2 className="text-200 text-text-primary-70">
        Version {updateStatus?.new_version}
      </h2>

      <p className="mt-1 mb-4 text-emphasis-300 text-text-primary">
        {updateStatus?.release_notes?.split("\n").map((line, index) => (
          <span key={index}>
            {line}
            <br />
          </span>
        ))}
      </p>
    </Modal>
  );
};

export default InfoModal;
