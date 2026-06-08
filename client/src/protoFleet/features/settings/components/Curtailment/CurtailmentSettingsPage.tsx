import { useCallback, useMemo, useState } from "react";
import { Navigate } from "react-router-dom";
import clsx from "clsx";

import type { CurtailmentHealth, CurtailmentSource } from "@/protoFleet/features/settings/components/Curtailment/types";
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
import Switch from "@/shared/components/Switch";
import { positions } from "@/shared/constants";
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
  stale: "bg-intent-warning-fill",
  offline: "bg-intent-critical-fill",
};

const formatSourceHealth = (health: CurtailmentSource["health"]) =>
  health
    .split("-")
    .map((word) => `${word.charAt(0).toUpperCase()}${word.slice(1)}`)
    .join(" ");

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

const SourceModal = ({ open, onDismiss }: { open: boolean; onDismiss: () => void }) => (
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
      },
      {
        text: "Save",
        variant: variants.primary,
      },
    ]}
    bodyClassName="text-text-primary"
  >
    <div className="grid gap-3 pb-2">
      <div className="grid gap-4 laptop:grid-cols-2">
        <Input id="source-name" label="Configuration name" />
        <Input id="source-type" label="Source type" initValue="MQTT" disabled />
      </div>
      <div className="grid gap-4 laptop:grid-cols-2">
        <Input id="source-host-primary" label="Broker host 1" />
        <Input id="source-host-backup" label="Broker host 2" />
      </div>
      <div className="grid gap-4 laptop:grid-cols-2">
        <Input
          id="source-port"
          label="Port"
          type="number"
          tooltip={{
            body: "Default MQTT port is 1883.",
            position: positions["top right"],
            widthClassName: "w-72",
          }}
        />
        <Input
          id="source-topic"
          label="Topic"
          tooltip={{
            body: "The MQTT topic to subscribe to for curtailment signals.",
            widthClassName: "w-72",
          }}
        />
      </div>
      <div className="grid gap-4 laptop:grid-cols-2">
        <Input id="source-username" label="Username" />
        <Input id="source-password" label="Password" type="password" />
      </div>
    </div>
  </Modal>
);

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
}: {
  onToggle: (sourceId: string) => void;
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
        <Switch checked={source.enabled} setChecked={() => onToggle(source.id)} />
      </div>
    ),
    width: "w-[6%] phone:w-9",
  },
});

type CurtailmentSettingsContentProps = {
  initialSources?: CurtailmentSource[];
  initialSourceModalOpen?: boolean;
};

export const CurtailmentSettingsContent = ({
  initialSources = [],
  initialSourceModalOpen = false,
}: CurtailmentSettingsContentProps) => {
  const [sources, setSources] = useState<CurtailmentSource[]>(() => [...initialSources]);
  const [isSourceModalOpen, setIsSourceModalOpen] = useState(initialSourceModalOpen);

  const toggleSource = useCallback((sourceId: string) => {
    setSources((current) =>
      current.map((source) => (source.id === sourceId ? { ...source, enabled: !source.enabled } : source)),
    );
  }, []);

  const colConfig = useMemo(
    () =>
      createCurtailmentSourceColConfig({
        onToggle: toggleSource,
      }),
    [toggleSource],
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
          emptyStateRow={<SourcesEmptyState />}
          applyColumnWidthsToCells
        />
      </section>

      <SourceModal open={isSourceModalOpen} onDismiss={() => setIsSourceModalOpen(false)} />
    </div>
  );
};

const CurtailmentSettingsPage = () => {
  const canManageCurtailment = useHasPermission("curtailment:manage");

  if (!canManageCurtailment) {
    return <Navigate to="/settings/general" replace />;
  }

  return <CurtailmentSettingsContent />;
};

export default CurtailmentSettingsPage;
