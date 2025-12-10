import { useCallback, useEffect, useMemo, useState } from "react";

import { poolInfoAttributes } from "./constants";
import { urlValidationErrors } from "./PoolForm/constants";
import { PoolConnectionTestProps, PoolIndex, PoolInfo } from "./types";

import { Alert, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import { DismissibleCalloutWrapper, intents } from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import { sizes } from "@/shared/components/Modal/constants";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
import { deepClone } from "@/shared/utils/utility";

interface PoolModalProps {
  onChangePools: (pools: PoolInfo[]) => void;
  onDismiss: () => void;
  poolIndex: PoolIndex;
  pools: PoolInfo[];
  show: boolean;
  isTestingConnection: boolean;
  testConnection: (args: PoolConnectionTestProps) => void;
  onSave?: (pool: PoolInfo, isPasswordSet: boolean) => Promise<void>;
  mode?: "add" | "edit";
}

const PoolModal = ({
  onChangePools,
  onDismiss,
  poolIndex,
  pools,
  show,
  isTestingConnection,
  testConnection,
  onSave,
  mode = "add",
}: PoolModalProps) => {
  const { isPhone, isTablet } = useWindowDimensions();
  const [draftPoolInfo, setDraftPoolInfo] = useState(deepClone(pools));
  const [urlError, setUrlError] = useState<string | undefined>();
  const [showCallout, setShowCallout] = useState(false);
  const [error, setError] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isPasswordSet, setIsPasswordSet] = useState(false);
  const [saveError, setSaveError] = useState(false);

  const modalSize = isPhone || isTablet ? sizes.fullscreen : sizes.large;

  const showNotConnectedCallout = useMemo(
    () => showCallout && !isTestingConnection && error,
    [showCallout, error, isTestingConnection],
  );

  const showConnectedCallout = useMemo(
    () => showCallout && !isTestingConnection && !error,
    [showCallout, error, isTestingConnection],
  );

  const showSaveErrorCallout = useMemo(() => saveError && !isSaving, [saveError, isSaving]);

  const isSaveDisabled = useMemo(() => !draftPoolInfo[poolIndex]?.url.trim(), [draftPoolInfo, poolIndex]);

  useEffect(() => {
    setDraftPoolInfo(deepClone(pools));
  }, [pools]);

  useEffect(() => {
    if (show) {
      setUrlError(undefined);
      setShowCallout(false);
      setError(false);
      setIsSaving(false);
      setIsPasswordSet(false);
      setSaveError(false);
    }
  }, [show]);

  const onPoolChange = useCallback(
    (value: string, id: string) => {
      setShowCallout(false);
      const infoKey = id.split(" ")[0];
      const poolsInfo = deepClone(draftPoolInfo);
      poolsInfo[poolIndex][infoKey] = value;
      setDraftPoolInfo(poolsInfo);

      if (infoKey === poolInfoAttributes.url) {
        setUrlError(value.trim() ? undefined : urlValidationErrors.required);
      }

      if (infoKey === poolInfoAttributes.password) {
        setIsPasswordSet(true);
      }
    },
    [draftPoolInfo, poolIndex],
  );

  const onSubmit = useCallback(async () => {
    onChangePools(draftPoolInfo);

    if (onSave) {
      setIsSaving(true);
      setSaveError(false);
      try {
        await onSave(draftPoolInfo[poolIndex], isPasswordSet);
        onDismiss();
      } catch (error) {
        console.error("Failed to save pool:", error);
        setSaveError(true);
      } finally {
        setIsSaving(false);
      }
    } else {
      onDismiss();
    }
  }, [draftPoolInfo, onChangePools, onDismiss, onSave, poolIndex, isPasswordSet]);

  const onTestConnection = useCallback(() => {
    if (!draftPoolInfo[poolIndex].url.trim()) {
      setUrlError(urlValidationErrors.required);
      return;
    }

    setError(false);
    testConnection({
      poolInfo: draftPoolInfo[poolIndex],
      onError: () => {
        setError(true);
      },
      onSuccess: () => {
        setError(false);
      },
      onFinally: () => setShowCallout(true),
    });
  }, [draftPoolInfo, poolIndex, testConnection]);

  if (!show) {
    return null;
  }

  return (
    <Modal
      buttons={[
        {
          text: "Test connection",
          onClick: onTestConnection,
          loading: isTestingConnection,
          variant: variants.secondary,
          className: "whitespace-nowrap overflow-clip",
        },
        {
          text: "Save",
          onClick: onSubmit,
          loading: isSaving,
          variant: variants.primary,
          testId: "pool-save-button",
          disabled: isSaveDisabled,
          dismissModalOnClick: false,
        },
      ]}
      contentHeader={mode === "add" ? "Add pool" : "Edit pool"}
      onDismiss={onDismiss}
      divider={false}
      size={modalSize}
    >
      <div className="mb-6 text-text-primary-70">Hashrate contributes to default mining pools.</div>
      <DismissibleCalloutWrapper
        icon={<Success />}
        intent={intents.success}
        onDismiss={() => setShowCallout(false)}
        show={showConnectedCallout}
        title="Pool connection successful"
        testId="pool-connected-callout"
      />
      <DismissibleCalloutWrapper
        icon={<Alert width={iconSizes.medium} />}
        intent={intents.danger}
        onDismiss={() => setShowCallout(false)}
        show={showNotConnectedCallout}
        title="We couldn't connect with your pool. Review your pool details and try again."
        testId="pool-not-connected-callout"
      />
      <DismissibleCalloutWrapper
        icon={<Alert width={iconSizes.medium} />}
        intent={intents.danger}
        onDismiss={() => setSaveError(false)}
        show={showSaveErrorCallout}
        title="Failed to save the pool. Please try again."
        testId="pool-save-error-callout"
      />
      <div className="space-y-4">
        <Input
          id={`${poolInfoAttributes.name} ${poolIndex}`}
          label="Pool Name"
          onChangeBlur={onPoolChange}
          initValue={draftPoolInfo[poolIndex].name || ""}
          testId={`${poolInfoAttributes.name}-${poolIndex}-input`}
        />
        <Input
          id={`${poolInfoAttributes.url} ${poolIndex}`}
          label="Pool URL"
          maxLength={2083}
          onChangeBlur={onPoolChange}
          initValue={draftPoolInfo[poolIndex].url}
          testId={`${poolInfoAttributes.url}-${poolIndex}-input`}
          error={urlError}
        />
        <Input
          id={`${poolInfoAttributes.username} ${poolIndex}`}
          label="Username"
          onChangeBlur={onPoolChange}
          initValue={draftPoolInfo[poolIndex].username}
          testId={`${poolInfoAttributes.username}-${poolIndex}-input`}
        />
        <Input
          id={`${poolInfoAttributes.password} ${poolIndex}`}
          label="Password (optional)"
          type="password"
          onChangeBlur={onPoolChange}
          initValue={draftPoolInfo[poolIndex].password}
          testId={`${poolInfoAttributes.password}-${poolIndex}-input`}
        />
      </div>
    </Modal>
  );
};

export default PoolModal;
