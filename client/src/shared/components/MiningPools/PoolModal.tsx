import { useCallback, useEffect, useState } from "react";

import { emptyPoolInfo } from "./constants";
import { WarnDeleteDialog, WarnDiscardDialog } from "./Dialogs";
import PoolForm from "./PoolForm";
import { PoolConnectionTestProps, PoolIndex, PoolInfo } from "./types";

import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import { animationDuration } from "@/shared/components/PageOverlay";
import { deepClone } from "@/shared/utils/utility";

interface PoolModalProps {
  onChangePools: (pools: PoolInfo[]) => void;
  onDismiss: () => void;
  poolIndex: PoolIndex;
  pools: PoolInfo[];
  show: boolean;
  isDefault?: boolean;
  isTestingConnection: boolean;
  testConnection: (args: PoolConnectionTestProps) => void;
}

const PoolModal = ({
  onChangePools,
  onDismiss,
  poolIndex,
  pools,
  show,
  isDefault = false,
  isTestingConnection,
  testConnection,
}: PoolModalProps) => {
  const [draftPoolInfo, setDraftPoolInfo] = useState(deepClone(pools));
  const [changed, setChanged] = useState(false);
  const [warnDiscard, setWarnDiscard] = useState(false);
  const [warnDelete, setWarnDelete] = useState(false);
  const [shouldTestConnection, setShouldTestConnection] = useState(false);

  useEffect(() => {
    setWarnDelete(false);
    setWarnDiscard(false);
    setChanged(false);
  }, [show]);

  useEffect(() => {
    setDraftPoolInfo(deepClone(pools));
  }, [pools]);

  const closeModal = useCallback(
    (submitted?: boolean) => {
      if (!submitted && changed) {
        setWarnDiscard(true);
        return;
      }
      onDismiss();
    },
    [changed, onDismiss],
  );

  const setDraftInfo = useCallback((poolsInfo: PoolInfo[]) => {
    setChanged(true);
    setDraftPoolInfo(poolsInfo);
  }, []);

  const onSubmit = useCallback(() => {
    onChangePools(draftPoolInfo);
  }, [draftPoolInfo, onChangePools]);

  const onDelete = useCallback(() => {
    closeModal(true);
    // since we show url and username in the confirmation modal
    // trigger the delete after animation of dialog closing is done
    setTimeout(() => {
      const currentInfo = draftPoolInfo;
      currentInfo[poolIndex] = deepClone(emptyPoolInfo);
      onChangePools(currentInfo);
    }, animationDuration);
  }, [closeModal, draftPoolInfo, poolIndex, onChangePools]);

  const onDiscard = useCallback(() => {
    closeModal(true);
    setDraftPoolInfo(deepClone(pools));
  }, [closeModal, pools]);

  return (
    <>
      {show && !warnDelete && !warnDiscard && (
        <Modal
          buttons={[
            {
              text: pools[poolIndex].url ? "Save" : "Add",
              onClick: onSubmit,
              variant: variants.primary,
              testId: "pool-save-button",
            },
            ...(pools[poolIndex].url && !isDefault
              ? [
                  {
                    text: "Delete",
                    onClick: () => setWarnDelete(true),
                    variant: variants.secondary,
                    testId: "pool-delete-button",
                  },
                ]
              : []),
            {
              text: "Test connection",
              onClick: () => setShouldTestConnection(true),
              loading: isTestingConnection,
              variant: variants.secondary,
              className: "whitespace-nowrap overflow-clip",
            },
          ]}
          contentHeader={
            isDefault ? "Default mining pool" : `Backup pool #${poolIndex}`
          }
          onDismiss={closeModal}
          divider={false}
        >
          <div className="mb-6">
            {isDefault
              ? "Your hashrate will contribute to your default mining pool."
              : "Backup pools are only used to mine if your default pool is unavailable."}
          </div>
          <PoolForm
            poolIndex={poolIndex}
            pools={draftPoolInfo}
            onChangePools={setDraftInfo}
            shouldTestConnection={shouldTestConnection}
            isTestingConnection={isTestingConnection}
            setShouldTestConnection={setShouldTestConnection}
            testConnection={testConnection}
          />
        </Modal>
      )}
      <WarnDeleteDialog
        poolInfo={pools[poolIndex]}
        keepBackup={() => setWarnDelete(false)}
        onDelete={onDelete}
        show={warnDelete}
      />
      <WarnDiscardDialog
        onDiscard={onDiscard}
        continueEditing={() => setWarnDiscard(false)}
        show={warnDiscard}
      />
    </>
  );
};

export default PoolModal;
