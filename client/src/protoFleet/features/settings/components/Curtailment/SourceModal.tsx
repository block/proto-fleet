const SOURCES_DESCRIPTION = "MaestroOS MQTT brokers that publish curtailment signals.";
import { type KeyboardEvent, type ReactElement, useCallback, useMemo, useState } from "react";

import {
  type CurtailmentSourceFormValues,
  DEFAULT_SOURCE_STALENESS_THRESHOLD_SEC,
  MAX_SOURCE_STALENESS_THRESHOLD_SEC,
} from "@/protoFleet/api/curtailmentTypes";

import { isInputEnterSaveEvent } from "@/protoFleet/features/settings/components/Curtailment/keyboard";
import { Alert, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import { DismissibleCalloutWrapper, intents } from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import { positions } from "@/shared/constants";
import "./CurtailmentSettingsPage.css";

const SOURCE_CONNECTION_FAILURE_MESSAGE =
  "We couldn't connect with your source. Review your source details and try again.";
const MAX_BROKER_PORT = 65_535;
const savedPasswordPlaceholder = "......";
export type SourceModalMode = "create" | "edit";
function getErrorMessage(error: unknown, fallbackMessage: string): string {
  return error instanceof Error && error.message ? error.message : fallbackMessage;
}
const emptySourceFormValues: CurtailmentSourceFormValues = {
  name: "",
  brokerPrimaryHost: "",
  brokerSecondaryHost: "",
  brokerPort: "",
  topic: "",
  username: "",
  password: "",
  stalenessThresholdSec: DEFAULT_SOURCE_STALENESS_THRESHOLD_SEC.toString(),
};

const sourceInputIds = {
  name: "source-name",
  brokerPrimaryHost: "source-host-primary",
  brokerSecondaryHost: "source-host-backup",
  brokerPort: "source-port",
  topic: "source-topic",
  username: "source-username",
  password: "source-password",
  stalenessThresholdSec: "source-staleness-threshold",
} as const;

const sourceInputIdToFormKey: Record<string, keyof CurtailmentSourceFormValues> = {
  [sourceInputIds.name]: "name",
  [sourceInputIds.brokerPrimaryHost]: "brokerPrimaryHost",
  [sourceInputIds.brokerSecondaryHost]: "brokerSecondaryHost",
  [sourceInputIds.brokerPort]: "brokerPort",
  [sourceInputIds.topic]: "topic",
  [sourceInputIds.username]: "username",
  [sourceInputIds.password]: "password",
  [sourceInputIds.stalenessThresholdSec]: "stalenessThresholdSec",
};

function isPositiveInteger(value: string): boolean {
  return /^[1-9]\d*$/.test(value.trim());
}

function sourceCredentialFieldsChanged(
  values: CurtailmentSourceFormValues,
  initialValues: CurtailmentSourceFormValues,
): boolean {
  return (
    values.brokerPrimaryHost.trim() !== initialValues.brokerPrimaryHost.trim() ||
    values.brokerSecondaryHost.trim() !== initialValues.brokerSecondaryHost.trim() ||
    values.brokerPort.trim() !== initialValues.brokerPort.trim() ||
    values.username.trim() !== initialValues.username.trim()
  );
}

function validateSourceFormValues(values: CurtailmentSourceFormValues, passwordRequired: boolean): SourceFormErrors {
  const errors: SourceFormErrors = {};

  if (values.name.trim() === "") {
    errors.name = "Enter a configuration name.";
  }
  if (values.brokerPrimaryHost.trim() === "") {
    errors.brokerPrimaryHost = "Enter broker host 1.";
  }
  if (values.brokerSecondaryHost.trim() === "") {
    errors.brokerSecondaryHost = "Enter broker host 2.";
  }
  if (values.topic.trim() === "") {
    errors.topic = "Enter a topic.";
  }
  if (values.username.trim() === "") {
    errors.username = "Enter a username.";
  }
  if (passwordRequired && values.password === "") {
    errors.password = "Enter a password.";
  }
  if (values.brokerPort.trim() === "") {
    errors.brokerPort = "Enter a port.";
  } else if (!isPositiveInteger(values.brokerPort)) {
    errors.brokerPort = "Enter port as a whole number greater than 0.";
  } else if (Number(values.brokerPort) > MAX_BROKER_PORT) {
    errors.brokerPort = `Enter port of ${MAX_BROKER_PORT.toLocaleString()} or less.`;
  }
  if (values.stalenessThresholdSec.trim() === "") {
    errors.stalenessThresholdSec = "Enter a timeout.";
  } else if (!isPositiveInteger(values.stalenessThresholdSec)) {
    errors.stalenessThresholdSec = "Enter timeout as a whole number greater than 0.";
  } else if (Number(values.stalenessThresholdSec) > MAX_SOURCE_STALENESS_THRESHOLD_SEC) {
    errors.stalenessThresholdSec = `Enter timeout of ${MAX_SOURCE_STALENESS_THRESHOLD_SEC.toLocaleString()} seconds or less.`;
  }

  return errors;
}

export type SourceModalProps = {
  open: boolean;
  mode?: SourceModalMode;
  initialValues?: CurtailmentSourceFormValues;
  hasSavedPassword?: boolean;
  onDismiss: () => void;
  onSave?: (values: CurtailmentSourceFormValues) => Promise<void>;
  onTestConnection?: (values: CurtailmentSourceFormValues) => Promise<void>;
  onDelete?: () => Promise<void>;
  saving?: boolean;
  testingConnection?: boolean;
  deleting?: boolean;
};

export type SourceFormErrors = Partial<Record<keyof CurtailmentSourceFormValues, string>>;
export type SourceValidationIntent = "save" | "testConnection";

export function SourceModal({
  open,
  mode = "create",
  initialValues = emptySourceFormValues,
  hasSavedPassword = false,
  onDismiss,
  onSave,
  onTestConnection,
  onDelete,
  saving = false,
  testingConnection = false,
  deleting = false,
}: SourceModalProps): ReactElement {
  const [values, setValues] = useState<CurtailmentSourceFormValues>(() => initialValues);
  const [passwordPlaceholderActive, setPasswordPlaceholderActive] = useState(() => mode === "edit" && hasSavedPassword);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [validationIntent, setValidationIntent] = useState<SourceValidationIntent | null>(null);
  const [showConnectionCallout, setShowConnectionCallout] = useState(false);
  const [connectionError, setConnectionError] = useState(false);
  const isEditMode = mode === "edit";
  const isBusy = saving || deleting || testingConnection;
  const passwordRequired = !isEditMode || sourceCredentialFieldsChanged(values, initialValues);
  const saveValidationErrors = useMemo(
    () => validateSourceFormValues(values, passwordRequired),
    [passwordRequired, values],
  );
  const testConnectionValidationErrors = useMemo(() => validateSourceFormValues(values, true), [values]);
  const visibleValidationErrors =
    validationIntent === "testConnection"
      ? testConnectionValidationErrors
      : validationIntent === "save"
        ? saveValidationErrors
        : {};
  const canSave = Object.keys(saveValidationErrors).length === 0;
  const canTestConnection = Object.keys(testConnectionValidationErrors).length === 0;
  const showSavedPasswordPlaceholder = isEditMode && hasSavedPassword && passwordPlaceholderActive;
  const passwordInputValue = showSavedPasswordPlaceholder ? savedPasswordPlaceholder : values.password;
  const showConnectionSuccessCallout = showConnectionCallout && !testingConnection && !connectionError;
  const showConnectionFailureCallout = showConnectionCallout && !testingConnection && connectionError;

  const updateSourceValue = useCallback((value: string, id: string) => {
    const formKey = sourceInputIdToFormKey[id];
    if (!formKey) {
      return;
    }

    setValues((currentValues) => ({
      ...currentValues,
      [formKey]: value,
    }));
    setShowConnectionCallout(false);
  }, []);

  const handlePasswordFocus = useCallback(() => {
    if (!showSavedPasswordPlaceholder) {
      return;
    }

    setPasswordPlaceholderActive(false);
    setValues((currentValues) => ({
      ...currentValues,
      password: "",
    }));
  }, [showSavedPasswordPlaceholder]);

  const handleSave = useCallback(async () => {
    if (isBusy) {
      return;
    }

    if (!canSave) {
      if (showSavedPasswordPlaceholder && saveValidationErrors.password) {
        setPasswordPlaceholderActive(false);
      }
      setValidationIntent("save");
      return;
    }

    try {
      setSaveError(null);
      await onSave?.(values);
      onDismiss();
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to save source."));
    }
  }, [canSave, isBusy, onDismiss, onSave, saveValidationErrors, showSavedPasswordPlaceholder, values]);

  const handleFormKeyDown = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      if (!isInputEnterSaveEvent(event)) {
        return;
      }

      event.preventDefault();
      void handleSave();
    },
    [handleSave],
  );

  const handleTestConnection = useCallback(async () => {
    if (isBusy || !onTestConnection) {
      return;
    }

    if (!canTestConnection) {
      if (showSavedPasswordPlaceholder && testConnectionValidationErrors.password) {
        setPasswordPlaceholderActive(false);
      }
      setValidationIntent("testConnection");
      return;
    }

    try {
      setSaveError(null);
      setConnectionError(false);
      await onTestConnection(values);
      setConnectionError(false);
    } catch {
      setConnectionError(true);
    } finally {
      setShowConnectionCallout(true);
    }
  }, [
    canTestConnection,
    isBusy,
    onTestConnection,
    showSavedPasswordPlaceholder,
    testConnectionValidationErrors,
    values,
  ]);

  const handleDelete = useCallback(async () => {
    if (!onDelete || isBusy) {
      return;
    }

    try {
      setSaveError(null);
      await onDelete();
      onDismiss();
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to delete source."));
    }
  }, [isBusy, onDelete, onDismiss]);

  return (
    <Modal
      open={open}
      title={isEditMode ? "Edit source" : "Add source"}
      description={SOURCES_DESCRIPTION}
      onDismiss={onDismiss}
      size={modalSizes.standard}
      divider={false}
      testId="curtailment-source-modal"
      buttons={[
        ...(isEditMode && onDelete
          ? [
              {
                text: "Delete",
                variant: variants.secondaryDanger,
                disabled: isBusy,
                loading: deleting,
                dismissModalOnClick: false,
                onClick: () => void handleDelete(),
              },
            ]
          : []),
        {
          text: "Test connection",
          variant: variants.secondary,
          className: "whitespace-nowrap overflow-clip",
          testId: "curtailment-source-test-connection-button",
          disabled: isBusy || !onTestConnection,
          loading: testingConnection,
          dismissModalOnClick: false,
          onClick: () => void handleTestConnection(),
        },
        {
          text: "Save",
          variant: variants.primary,
          disabled: isBusy,
          loading: saving,
          dismissModalOnClick: false,
          onClick: () => void handleSave(),
        },
      ]}
      bodyClassName="text-text-primary"
    >
      <div className="grid gap-3 pb-2" onKeyDown={handleFormKeyDown}>
        <DismissibleCalloutWrapper
          icon={<Success />}
          intent={intents.success}
          onDismiss={() => setShowConnectionCallout(false)}
          show={showConnectionSuccessCallout}
          title="Source connection successful"
          testId="curtailment-source-connected-callout"
        />
        <DismissibleCalloutWrapper
          icon={<Alert width={iconSizes.medium} />}
          intent={intents.danger}
          onDismiss={() => setShowConnectionCallout(false)}
          show={showConnectionFailureCallout}
          title={SOURCE_CONNECTION_FAILURE_MESSAGE}
          testId="curtailment-source-not-connected-callout"
        />
        {saveError ? (
          <div className="rounded-lg bg-intent-critical-10 px-4 py-3 text-300 text-text-critical">{saveError}</div>
        ) : null}
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.name}
            label="Configuration name"
            initValue={values.name}
            error={visibleValidationErrors.name}
            onChange={updateSourceValue}
          />
          <Input id="source-type" label="Integration" initValue="MaestroOS" disabled />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.brokerPrimaryHost}
            label="Broker host 1"
            initValue={values.brokerPrimaryHost}
            error={visibleValidationErrors.brokerPrimaryHost}
            onChange={updateSourceValue}
          />
          <Input
            id={sourceInputIds.brokerSecondaryHost}
            label="Broker host 2"
            initValue={values.brokerSecondaryHost}
            error={visibleValidationErrors.brokerSecondaryHost}
            onChange={updateSourceValue}
          />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.brokerPort}
            label="Port"
            type="number"
            inputMode="numeric"
            initValue={values.brokerPort}
            error={visibleValidationErrors.brokerPort}
            onChange={updateSourceValue}
            tooltip={{
              body: "Default MQTT port for MaestroOS is 1883.",
              position: positions["top right"],
              widthClassName: "w-72",
            }}
          />
          <Input
            id={sourceInputIds.topic}
            label="Topic"
            initValue={values.topic}
            error={visibleValidationErrors.topic}
            onChange={updateSourceValue}
            tooltip={{
              body: "The MQTT topic to subscribe to on MaestroOS for curtailment signals.",
              widthClassName: "w-72",
            }}
          />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.username}
            label="Username"
            initValue={values.username}
            error={visibleValidationErrors.username}
            onChange={updateSourceValue}
          />
          <Input
            id={sourceInputIds.password}
            label="Password"
            type="password"
            initValue={passwordInputValue}
            error={visibleValidationErrors.password}
            onChange={updateSourceValue}
            onFocus={handlePasswordFocus}
            hidePasswordToggle={showSavedPasswordPlaceholder}
          />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.stalenessThresholdSec}
            label="No signal timeout"
            type="number"
            inputMode="numeric"
            initValue={values.stalenessThresholdSec}
            error={visibleValidationErrors.stalenessThresholdSec}
            onChange={updateSourceValue}
            units="sec"
            tooltip={{
              body: "When no MQTT signal is received for this duration, the source is treated as OFF.",
              widthClassName: "w-72",
            }}
          />
        </div>
      </div>
    </Modal>
  );
}
