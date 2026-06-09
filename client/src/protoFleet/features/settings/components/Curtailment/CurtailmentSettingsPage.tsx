import { useCallback, useEffect, useMemo, useState } from "react";
import { Navigate } from "react-router-dom";
import clsx from "clsx";

import useMqttCurtailmentSources from "@/protoFleet/api/useMqttCurtailmentSources";
import type {
  CurtailmentHealth,
  CurtailmentSource,
  CurtailmentSourceFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import { useHasPermission } from "@/protoFleet/store";
import { Info } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Switch from "@/shared/components/Switch";
import { positions } from "@/shared/constants";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { classNameToSelectors } from "@/shared/utils/cssUtils";
import "./CurtailmentSettingsPage.css";

const CURTAILMENT_PAGE_DESCRIPTION =
  "Configure response profiles, manage external signal sources, and define automations that trigger curtailment.";
const SOURCES_DESCRIPTION = "External systems that send curtailment signals via MQTT.";

const curtailmentSourceCols = {
  name: "name",
  lastSignalValue: "lastSignalValue",
  lastSignalUpdate: "lastSignalUpdate",
  health: "health",
  enabled: "enabled",
} as const;

type CurtailmentSourceColumn = (typeof curtailmentSourceCols)[keyof typeof curtailmentSourceCols];

const activeCurtailmentSourceCols: CurtailmentSourceColumn[] = [
  curtailmentSourceCols.name,
  curtailmentSourceCols.lastSignalValue,
  curtailmentSourceCols.lastSignalUpdate,
  curtailmentSourceCols.health,
  curtailmentSourceCols.enabled,
];

const curtailmentSourceColTitles: ColTitles<CurtailmentSourceColumn> = {
  name: "Name",
  lastSignalValue: "Last signal",
  lastSignalUpdate: "Updated",
  health: "Connection",
  enabled: "",
};

const curtailmentSourceColumnAriaLabels: Partial<Record<CurtailmentSourceColumn, string>> = {
  enabled: "Enabled",
};

const curtailmentSourceColumnsExemptFromDisabledStyling = new Set<CurtailmentSourceColumn>([
  curtailmentSourceCols.enabled,
]);

const curtailmentSourcesTableClassName = [
  "mb-2 w-full",
  "phone:table-fixed",
  "[&_thead_th]:text-text-primary-50",
  "phone:[&_thead_th:last-child]:w-9",
  "phone:[&_thead_th:last-child>div]:w-9",
].join(" ");

const sourceHealthDotClassName: Record<CurtailmentHealth, string> = {
  connected: "bg-intent-success-fill",
  noSignal: "bg-intent-warning-fill",
  offline: "bg-intent-critical-fill",
};

const emptySourceFormValues: CurtailmentSourceFormValues = {
  name: "",
  brokerPrimaryHost: "",
  brokerSecondaryHost: "",
  brokerPort: "",
  topic: "",
  username: "",
  password: "",
};

const emptyCurtailmentSources: CurtailmentSource[] = [];

const sourceInputIds = {
  name: "source-name",
  brokerPrimaryHost: "source-host-primary",
  brokerSecondaryHost: "source-host-backup",
  brokerPort: "source-port",
  topic: "source-topic",
  username: "source-username",
  password: "source-password",
} as const;

const sourceInputIdToFormKey: Record<string, keyof CurtailmentSourceFormValues> = {
  [sourceInputIds.name]: "name",
  [sourceInputIds.brokerPrimaryHost]: "brokerPrimaryHost",
  [sourceInputIds.brokerSecondaryHost]: "brokerSecondaryHost",
  [sourceInputIds.brokerPort]: "brokerPort",
  [sourceInputIds.topic]: "topic",
  [sourceInputIds.username]: "username",
  [sourceInputIds.password]: "password",
};

const isPositiveInteger = (value: string) => /^[1-9]\d*$/.test(value.trim());

const isSourceFormValid = (values: CurtailmentSourceFormValues) =>
  values.name.trim() !== "" &&
  values.brokerPrimaryHost.trim() !== "" &&
  values.brokerSecondaryHost.trim() !== "" &&
  values.topic.trim() !== "" &&
  values.username.trim() !== "" &&
  values.password !== "" &&
  isPositiveInteger(values.brokerPort) &&
  Number(values.brokerPort) <= 65535;

const getErrorMessage = (error: unknown, fallbackMessage: string) =>
  error instanceof Error && error.message ? error.message : fallbackMessage;

const sourceHealthLabel: Record<CurtailmentHealth, string> = {
  connected: "Connected",
  noSignal: "No signal",
  offline: "Offline",
};

const formatSourceHealth = (health: CurtailmentSource["health"]) => sourceHealthLabel[health];

const SOURCES_INFO_TRIGGER_CLASS_NAME = "curtailment-sources-info-trigger";

const SourcesInfoToggleContent = () => {
  const [isOpen, setIsOpen] = useState(false);
  const { triggerRef } = usePopover();
  const closeIgnoreSelectors = classNameToSelectors(SOURCES_INFO_TRIGGER_CLASS_NAME);

  return (
    <div ref={triggerRef} className={`${SOURCES_INFO_TRIGGER_CLASS_NAME} relative`}>
      <Button
        variant={variants.secondary}
        size={sizes.compact}
        ariaHasPopup
        ariaExpanded={isOpen}
        ariaLabel="About sources"
        prefixIcon={<Info width={iconSizes.small} className="text-text-primary-70" />}
        onClick={() => setIsOpen((current) => !current)}
      />
      {isOpen ? (
        <Popover
          position={positions["bottom left"]}
          size={popoverSizes.normal}
          offset={8}
          className="!space-y-0"
          closePopover={() => setIsOpen(false)}
          closeIgnoreSelectors={closeIgnoreSelectors}
          testId="curtailment-sources-info-popover"
        >
          <p className="text-300 text-text-primary-70">{SOURCES_DESCRIPTION}</p>
        </Popover>
      ) : null}
    </div>
  );
};

const SourcesInfoToggle = () => (
  <PopoverProvider>
    <SourcesInfoToggleContent />
  </PopoverProvider>
);

const SourcesEmptyState = () => (
  <div className="flex min-h-[220px] w-full flex-col items-center justify-center py-14 text-center">
    <div className="text-heading-200 text-text-primary">No sources configured</div>
    <p className="mt-1 text-400 text-text-primary-70">Add a source to receive curtailment signals via MQTT.</p>
  </div>
);

const SourcesLoadingState = () => (
  <div className="flex min-h-[220px] w-full items-center justify-center py-14">
    <ProgressCircular indeterminate />
  </div>
);

const SourcesErrorState = ({ message }: { message: string }) => (
  <div className="flex min-h-[220px] w-full flex-col items-center justify-center py-14 text-center">
    <div className="text-heading-200 text-text-primary">Unable to load sources</div>
    <p className="mt-1 text-400 text-text-primary-70">{message}</p>
  </div>
);

type SourceModalProps = {
  open: boolean;
  onDismiss: () => void;
  onSave?: (values: CurtailmentSourceFormValues) => Promise<void>;
  saving?: boolean;
};

const SourceModal = ({ open, onDismiss, onSave, saving = false }: SourceModalProps) => {
  const [values, setValues] = useState<CurtailmentSourceFormValues>(emptySourceFormValues);
  const [saveError, setSaveError] = useState<string | null>(null);
  const canSave = isSourceFormValid(values);

  const updateSourceValue = useCallback((value: string, id: string) => {
    const formKey = sourceInputIdToFormKey[id];
    if (!formKey) {
      return;
    }

    setValues((currentValues) => ({
      ...currentValues,
      [formKey]: value,
    }));
  }, []);

  const handleSave = useCallback(async () => {
    if (!canSave || saving) {
      return;
    }

    try {
      setSaveError(null);
      await onSave?.(values);
      onDismiss();
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to save source."));
    }
  }, [canSave, onDismiss, onSave, saving, values]);

  return (
    <Modal
      open={open}
      title="Add source"
      description={SOURCES_DESCRIPTION}
      onDismiss={onDismiss}
      size={modalSizes.standard}
      divider={false}
      testId="curtailment-source-modal"
      buttons={[
        {
          text: "Test connection",
          variant: variants.secondary,
          className: "whitespace-nowrap overflow-clip",
          testId: "curtailment-source-test-connection-button",
          disabled: true,
          dismissModalOnClick: false,
        },
        {
          text: "Save",
          variant: variants.primary,
          disabled: !canSave || saving,
          loading: saving,
          dismissModalOnClick: false,
          onClick: () => void handleSave(),
        },
      ]}
      bodyClassName="text-text-primary"
    >
      <div className="grid gap-3 pb-2">
        {saveError ? (
          <div className="rounded-lg bg-intent-critical-10 px-4 py-3 text-300 text-text-critical">{saveError}</div>
        ) : null}
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.name}
            label="Configuration name"
            initValue={values.name}
            onChange={updateSourceValue}
          />
          <Input id="source-type" label="Source type" initValue="MQTT" disabled />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.brokerPrimaryHost}
            label="Broker host 1"
            initValue={values.brokerPrimaryHost}
            onChange={updateSourceValue}
          />
          <Input
            id={sourceInputIds.brokerSecondaryHost}
            label="Broker host 2"
            initValue={values.brokerSecondaryHost}
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
            onChange={updateSourceValue}
            tooltip={{
              body: "Default MQTT port is 1883.",
              position: positions["top right"],
              widthClassName: "w-72",
            }}
          />
          <Input
            id={sourceInputIds.topic}
            label="Topic"
            initValue={values.topic}
            onChange={updateSourceValue}
            tooltip={{
              body: "The MQTT topic to subscribe to for curtailment signals.",
              widthClassName: "w-72",
            }}
          />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.username}
            label="Username"
            initValue={values.username}
            onChange={updateSourceValue}
          />
          <Input
            id={sourceInputIds.password}
            label="Password"
            type="password"
            initValue={values.password}
            onChange={updateSourceValue}
          />
        </div>
      </div>
    </Modal>
  );
};

const SectionHeader = ({
  title,
  buttonText,
  onButtonClick,
}: {
  title: string;
  buttonText: string;
  onButtonClick: () => void;
}) => (
  <div className="curtailment-section-header">
    <div className="curtailment-section-header__title">
      <h2 className="curtailment-section-header__label">{title}</h2>
    </div>
    <div className="flex shrink-0 items-center gap-2">
      <SourcesInfoToggle />
      <Button
        variant={variants.secondary}
        size={sizes.compact}
        text={buttonText}
        onClick={onButtonClick}
        className="curtailment-settings__action-button"
      />
    </div>
  </div>
);

const createCurtailmentSourceColConfig = ({
  onToggle,
  updatingSourceIds,
}: {
  onToggle: (sourceId: string) => void;
  updatingSourceIds: Set<string>;
}): ColConfig<CurtailmentSource, string, CurtailmentSourceColumn> => ({
  [curtailmentSourceCols.name]: {
    component: (source) => (
      <span className="block max-w-full truncate text-emphasis-300 text-text-primary">{source.name}</span>
    ),
    width: "w-[34%] phone:w-auto",
  },
  [curtailmentSourceCols.lastSignalValue]: {
    component: (source) => <span className="truncate text-text-primary">{source.lastTarget}</span>,
    width: "w-[20%] phone:w-auto",
  },
  [curtailmentSourceCols.lastSignalUpdate]: {
    component: (source) => <span className="truncate text-text-primary">{source.lastSeen}</span>,
    width: "w-[20%] phone:w-auto",
  },
  [curtailmentSourceCols.health]: {
    component: (source) => (
      <div className="inline-flex items-center gap-1.5">
        <span className={clsx("h-2 w-2 shrink-0 rounded-full", sourceHealthDotClassName[source.health])} />
        <span className="truncate text-text-primary">{formatSourceHealth(source.health)}</span>
      </div>
    ),
    width: "w-[20%] phone:w-auto",
  },
  [curtailmentSourceCols.enabled]: {
    component: (source) => (
      <div className="flex justify-end" data-interactive>
        <Switch
          checked={source.enabled}
          setChecked={() => onToggle(source.id)}
          disabled={updatingSourceIds.has(source.id)}
        />
      </div>
    ),
    width: "w-[6%] phone:w-9",
  },
});

type CurtailmentSettingsContentProps = {
  initialSources?: CurtailmentSource[];
  initialSourceModalOpen?: boolean;
  sources?: CurtailmentSource[];
  isLoadingSources?: boolean;
  loadSourcesError?: string | null;
  isSavingSource?: boolean;
  updatingSourceIds?: Set<string>;
  onCreateSource?: (values: CurtailmentSourceFormValues) => Promise<CurtailmentSource | void>;
  onToggleSource?: (source: CurtailmentSource, enabled: boolean) => Promise<CurtailmentSource | void>;
};

export const CurtailmentSettingsContent = ({
  initialSources = emptyCurtailmentSources,
  initialSourceModalOpen = false,
  sources: controlledSources,
  isLoadingSources = false,
  loadSourcesError = null,
  isSavingSource = false,
  updatingSourceIds = new Set<string>(),
  onCreateSource,
  onToggleSource,
}: CurtailmentSettingsContentProps) => {
  const [localSources, setLocalSources] = useState<CurtailmentSource[]>(() => [...initialSources]);
  const [isSourceModalOpen, setIsSourceModalOpen] = useState(initialSourceModalOpen);
  const sources = controlledSources ?? localSources;

  const toggleSource = useCallback(
    (sourceId: string) => {
      const source = sources.find((currentSource) => currentSource.id === sourceId);
      if (!source) {
        return;
      }

      const nextEnabled = !source.enabled;
      if (onToggleSource) {
        void onToggleSource(source, nextEnabled);
        return;
      }

      setLocalSources((currentSources) =>
        currentSources.map((currentSource) =>
          currentSource.id === sourceId ? { ...currentSource, enabled: nextEnabled } : currentSource,
        ),
      );
    },
    [onToggleSource, sources],
  );

  const handleCreateSource = useCallback(
    async (values: CurtailmentSourceFormValues) => {
      const createdSource = await onCreateSource?.(values);
      if (!controlledSources && createdSource) {
        setLocalSources((currentSources) => [
          ...currentSources.filter((currentSource) => currentSource.id !== createdSource.id),
          createdSource,
        ]);
      }
    },
    [controlledSources, onCreateSource],
  );

  const colConfig = useMemo(
    () =>
      createCurtailmentSourceColConfig({
        onToggle: toggleSource,
        updatingSourceIds,
      }),
    [toggleSource, updatingSourceIds],
  );

  const emptyStateRow = loadSourcesError ? (
    <SourcesErrorState message={loadSourcesError} />
  ) : isLoadingSources ? (
    <SourcesLoadingState />
  ) : (
    <SourcesEmptyState />
  );

  return (
    <div className="flex flex-col gap-14" data-testid="settings-curtailment-page">
      <Header title="Curtailment" titleSize="text-heading-300" description={CURTAILMENT_PAGE_DESCRIPTION} />

      <section className="curtailment-settings__section curtailment-settings__section--last">
        <SectionHeader title="Sources" buttonText="Add source" onButtonClick={() => setIsSourceModalOpen(true)} />
        <List<CurtailmentSource, string, CurtailmentSourceColumn>
          activeCols={activeCurtailmentSourceCols}
          colTitles={curtailmentSourceColTitles}
          columnHeaderAriaLabels={curtailmentSourceColumnAriaLabels}
          colConfig={colConfig}
          items={sources}
          itemKey="id"
          total={sources.length}
          hideTotal
          itemName={{ singular: "source", plural: "sources" }}
          stickyFirstColumn={false}
          isRowDisabled={(source) => !source.enabled}
          columnsExemptFromDisabledStyling={curtailmentSourceColumnsExemptFromDisabledStyling}
          tableClassName={curtailmentSourcesTableClassName}
          emptyStateRow={emptyStateRow}
          applyColumnWidthsToCells
        />
      </section>

      <SourceModal
        key={isSourceModalOpen ? "source-modal-open" : "source-modal-closed"}
        open={isSourceModalOpen}
        onDismiss={() => setIsSourceModalOpen(false)}
        onSave={handleCreateSource}
        saving={isSavingSource}
      />
    </div>
  );
};

const CurtailmentSettingsPage = () => {
  const canManageCurtailment = useHasPermission("curtailment:manage");
  const { sources, isLoading, isCreating, updatingSourceIds, loadError, createSource, setSourceEnabled } =
    useMqttCurtailmentSources(canManageCurtailment);

  useEffect(() => {
    if (!loadError) {
      return;
    }

    pushToast({
      message: loadError,
      status: STATUSES.error,
    });
  }, [loadError]);

  const handleCreateSource = useCallback(
    async (values: CurtailmentSourceFormValues) => {
      const source = await createSource(values);
      pushToast({
        message: "Source added",
        status: STATUSES.success,
      });
      return source;
    },
    [createSource],
  );

  const handleToggleSource = useCallback(
    async (source: CurtailmentSource, enabled: boolean) => {
      try {
        return await setSourceEnabled(source.id, enabled);
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to update source."),
          status: STATUSES.error,
        });
        throw error;
      }
    },
    [setSourceEnabled],
  );

  if (!canManageCurtailment) {
    return <Navigate to="/settings/general" replace />;
  }

  return (
    <CurtailmentSettingsContent
      sources={sources}
      isLoadingSources={isLoading}
      loadSourcesError={loadError}
      isSavingSource={isCreating}
      updatingSourceIds={updatingSourceIds}
      onCreateSource={handleCreateSource}
      onToggleSource={handleToggleSource}
    />
  );
};

export default CurtailmentSettingsPage;
