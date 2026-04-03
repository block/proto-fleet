import { useCallback, useEffect, useRef, useState } from "react";
import { useApiKeys } from "@/protoFleet/api/useApiKeys";
import type { ApiKeyItem } from "@/protoFleet/api/useApiKeys";
import { Copy, Success } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

interface CreateApiKeyModalProps {
  open?: boolean;
  onDismiss: () => void;
  onSuccess: () => void;
}

type ModalStep = "enterDetails" | "displayKey";

const CreateApiKeyModal = ({ open, onDismiss, onSuccess }: CreateApiKeyModalProps) => {
  const isVisible = open ?? true;
  const { createApiKey } = useApiKeys();
  const [step, setStep] = useState<ModalStep>("enterDetails");
  const [name, setName] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [fullKey, setFullKey] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");
  const createRequestIDRef = useRef(0);
  const isMountedRef = useRef(true);

  useEffect(() => {
    return () => {
      isMountedRef.current = false;
      createRequestIDRef.current += 1;
    };
  }, []);

  useEffect(() => {
    if (isVisible) {
      return;
    }

    createRequestIDRef.current += 1;

    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset modal state on close
    setStep("enterDetails");
    setName("");
    setExpiresAt("");
    setFullKey("");
    setIsSubmitting(false);
    setErrorMsg("");
  }, [isVisible]);

  const handleDismiss = useCallback(() => {
    createRequestIDRef.current += 1;
    onDismiss();
  }, [onDismiss]);

  const handleCreate = useCallback(() => {
    if (!name.trim()) {
      setErrorMsg("Name is required");
      return;
    }

    if (expiresAt) {
      // Compare in local time — the date picker yields a local calendar date
      const now = new Date();
      const localToday = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
      if (expiresAt <= localToday) {
        setErrorMsg("Expiration date must be after today");
        return;
      }
    }

    setIsSubmitting(true);
    setErrorMsg("");
    const createRequestID = createRequestIDRef.current + 1;
    createRequestIDRef.current = createRequestID;

    createApiKey({
      name: name.trim(),
      // Interpret as local end-of-day so the key expires when the user expects
      expiresAt: expiresAt ? new Date(expiresAt + "T23:59:59") : undefined,
      onSuccess: (apiKey: string, _info: ApiKeyItem) => {
        if (!isMountedRef.current || createRequestIDRef.current !== createRequestID) {
          return;
        }

        setFullKey(apiKey);
        setStep("displayKey");
        pushToast({
          message: `API key "${name}" created successfully`,
          status: STATUSES.success,
        });
      },
      onError: (error: string) => {
        if (!isMountedRef.current || createRequestIDRef.current !== createRequestID) {
          return;
        }

        setErrorMsg(error || "Failed to create API key. Please try again.");
      },
      onFinally: () => {
        if (!isMountedRef.current || createRequestIDRef.current !== createRequestID) {
          return;
        }

        setIsSubmitting(false);
      },
    });
  }, [name, expiresAt, createApiKey]);

  const handleCopyKey = useCallback(() => {
    copyToClipboard(fullKey)
      .then(() => {
        pushToast({
          message: "API key copied to clipboard",
          status: STATUSES.success,
        });
      })
      .catch(() => {
        pushToast({
          message: "Failed to copy API key",
          status: STATUSES.error,
        });
      });
  }, [fullKey]);

  const handleDone = useCallback(() => {
    onSuccess();
    onDismiss();
  }, [onSuccess, onDismiss]);

  if (step === "enterDetails") {
    return (
      <Modal
        open={isVisible}
        onDismiss={handleDismiss}
        size="small"
        contentHeader="Create API key"
        buttons={[
          {
            text: "Create",
            onClick: handleCreate,
            variant: variants.primary,
            loading: isSubmitting,
            dismissModalOnClick: false,
          },
        ]}
        divider={false}
      >
        <div className="mb-6">
          Create a named API key for programmatic access to the Fleet gRPC API. The key will be shown once after
          creation.
        </div>

        {errorMsg ? (
          <div className="mb-6 rounded-lg bg-intent-critical-10 px-3 py-2 text-emphasis-300 text-intent-critical-text">
            {errorMsg}
          </div>
        ) : null}

        <div className="flex flex-col gap-4">
          <Input
            id="api-key-name"
            label="Key name"
            initValue={name}
            onChange={(value) => {
              setName(value);
              setErrorMsg("");
            }}
            autoFocus
          />
          <Input
            id="api-key-expires"
            label="Expiration date (optional)"
            type="date"
            initValue={expiresAt}
            onChange={(value) => setExpiresAt(value)}
          />
        </div>
      </Modal>
    );
  }

  return (
    <Dialog
      open={isVisible}
      title="API key created"
      titleSize="text-heading-300"
      subtitle="Copy this key now and store it securely. It won't be shown again."
      subtitleSize="text-300"
      onDismiss={handleDone}
      icon={
        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-surface-5 text-intent-success-fill">
          <Success />
        </div>
      }
      buttonGroupVariant={groupVariants.rightAligned}
      buttons={[
        {
          text: "Done",
          onClick: handleDone,
          variant: variants.primary,
        },
      ]}
    >
      <div className="flex items-center justify-between gap-2 rounded-xl bg-core-primary-5 px-6 py-6">
        <div className="font-mono text-300 break-all text-text-primary" data-testid="api-key-value">
          {fullKey}
        </div>
        <button onClick={handleCopyKey} className="shrink-0 text-text-primary" aria-label="Copy API key">
          <Copy />
        </button>
      </div>
    </Dialog>
  );
};

export default CreateApiKeyModal;
