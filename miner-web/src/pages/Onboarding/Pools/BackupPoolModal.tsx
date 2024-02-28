import { useCallback, useEffect, useState } from "react";

import { useTestConnection } from "api";

import { deepClone } from "common/utils/utility";

import { variants } from "components/Button";
import Modal from "components/Modal";
import { animationDuration } from "components/PageOverlay";

import { emptyPoolInfo } from "../constants";
import { WarnDeleteDialog, WarnDiscardDialog } from "../Dialogs";
import { PoolIndex, PoolInfo } from "../types";
import PoolForm from "./PoolForm";

interface BackupPoolProps {
  onChangePools: (pools: PoolInfo[]) => void;
  onDismiss: () => void;
  poolIndex: PoolIndex;
  pools: PoolInfo[];
  show: boolean;
}

const BackupPoolModal = ({
  onChangePools,
  onDismiss,
  poolIndex,
  pools,
  show,
}: BackupPoolProps) => {
  const [draftPoolInfo, setDraftPoolInfo] = useState(deepClone(pools));
  const [changed, setChanged] = useState(false);
  const [warnDiscard, setWarnDiscard] = useState(false);
  const [warnDelete, setWarnDelete] = useState(false);
  const [shouldTestConnection, setShouldTestConnection] = useState(false);
  const { testConnection, pending: isTestingConnection } = useTestConnection();

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
    [changed, onDismiss]
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
            },
            ...(pools[poolIndex].url
              ? [
                  {
                    text: "Delete",
                    onClick: () => setWarnDelete(true),
                    variant: variants.secondary,
                  },
                ]
              : []),
            {
              text: "Test connection",
              onClick: () => setShouldTestConnection(true),
              loading: isTestingConnection,
              variant: variants.secondary,
            },
          ]}
          contentHeader={`Backup pool #${poolIndex}`}
          onDismiss={closeModal}
        >
          <div className="mb-6">
            Backup pools are only used to mine if your default pool is
            unavailable.
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

export default BackupPoolModal;
