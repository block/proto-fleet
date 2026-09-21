import { useState } from "react";
import { useComponentHardware, useComponentTelemetry } from "./hooks";
import { miningStatusLabels } from "./scopeHealth";
import type { ProtoOSStatusModalProps } from "./types";
import { useScopeHealth } from "./useScopeHealth";
import { buildComponentStatusProps, getComponentTitle, transformErrorsForModal } from "./utils";
import { useCoolingStatus } from "@/protoOS/api/hooks/useCoolingStatus";
import { useErrors } from "@/protoOS/api/hooks/useErrors";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import { WakingDialog } from "@/protoOS/components/Power";
import { useWakeMiner } from "@/protoOS/hooks/useWakeMiner";
import type { ComponentRef } from "@/protoOS/store/contract";
import { useDetailedErrors, useIsSleeping } from "@/protoOS/store/hooks/useMinerStatus";
import { useTemperatureUnit } from "@/protoOS/store/hooks/useUI";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { componentName, resolveComponentAddress } from "@/protoOS/store/utils/componentSelection";
import { Alert, ArrowRight, Info, Success } from "@/shared/assets/icons";
import { DialogIcon } from "@/shared/components/Dialog";
import Modal from "@/shared/components/Modal";
import ComponentStatusModalContent from "@/shared/components/StatusModal/ComponentStatusModalContent";
import StatusModalLayout from "@/shared/components/StatusModal/StatusModalLayout";
import { convertCtoF } from "@/shared/utils/telemetryFormat";

const ContainerHealthModal = ({
  open = true,
  onClose,
  scope = { kind: "container" },
  componentAddress,
  showBackButton = true,
  onWake,
  isSleeping = false,
}: ProtoOSStatusModalProps & { onWake?: () => void; isSleeping?: boolean }) => {
  const hardware = useMinerStore((state) => state.hardware);
  const [component, setComponent] = useState(() => resolveComponentAddress(componentAddress, hardware));
  const summary = useScopeHealth(scope);
  const errors = useDetailedErrors();
  const refresh = useErrors({ enabled: open, poll: open, pollIntervalMs: 15_000 });
  useCoolingStatus({ enabled: open, poll: open });
  const readings = useTelemetry({
    level: ["miner", "hashboard", "psu"],
    enabled: open,
    poll: open,
    pollIntervalMs: 15_000,
  });
  const telemetry = useComponentTelemetry(component);
  const componentHardware = useComponentHardware(component);
  const name = (ref: ComponentRef) => componentName(ref, hardware);
  const detail = component
    ? buildComponentStatusProps(component, name(component), errors, telemetry, componentHardware)
    : undefined;
  if (detail && component?.kind === "hashboard") {
    const board = telemetry && "hashboardId" in telemetry ? telemetry : undefined;
    detail.metadata = {
      ...detail.metadata,
      miningStatus: { label: "Mining status", value: miningStatusLabels[board?.miningStatus ?? "unknown"] },
      isolation: {
        label: "Isolation",
        value: board?.isolated === undefined ? "Unknown" : board.isolated ? "Isolated" : "Not isolated",
      },
    };
  }
  const back = component && showBackButton ? () => setComponent(undefined) : undefined;
  const dismiss = back ?? onClose;
  const title =
    detail && component
      ? `${getComponentTitle(detail.componentType)} · ${name(component)}`
      : `${summary?.label ?? "Container"} status`;
  const rows = summary
    ? [
        ...transformErrorsForModal(summary.errors, name, setComponent),
        ...summary.observations.map(({ component: ref, message }) => ({
          componentName: name(ref),
          message,
          onClick: () => setComponent(ref),
        })),
      ].sort((a, b) => a.componentName.localeCompare(b.componentName, undefined, { numeric: true }))
    : [];
  const rollup = summary?.rollup;
  const temperatureUnit = useTemperatureUnit();
  const temperature = rollup?.averageAsicTemperature?.latest?.value;
  const power = rollup?.power?.latest?.value;
  const temperatureText =
    temperature == null || !Number.isFinite(temperature)
      ? "—"
      : `${(temperatureUnit === "F" ? convertCtoF(temperature) : temperature).toFixed(1)}°${temperatureUnit}`;
  const metrics = [
    {
      label: "Online hashboards",
      value:
        rollup?.boardCount === undefined || rollup.onlineBoardCount === undefined
          ? "—"
          : `${rollup.onlineBoardCount}/${rollup.boardCount}`,
    },
    { label: "Components needing attention", value: summary?.affectedComponents ?? "—" },
    { label: "Temperature", value: temperatureText },
    { label: "Power", value: power == null || !Number.isFinite(power) ? "—" : `${(power / 1_000).toFixed(1)} kW` },
  ];
  return (
    <Modal
      open={open}
      manageFocus
      title={title}
      dialogLabel={title}
      navigationKey={component ? `${component.kind}:${component.id}` : "summary"}
      icon={back ? <ArrowRight className="rotate-180" /> : undefined}
      iconAriaLabel={back ? `Back to ${summary?.label ?? "container"}` : "Close dialog"}
      onIconClick={back}
      onDismiss={dismiss}
      buttons={[
        ...(!component && scope.kind === "container" && onWake
          ? [{ text: "Wake miner", variant: "secondary" as const, onClick: onWake }]
          : []),
        { text: "Done", variant: "primary", onClick: dismiss, dismissModalOnClick: false },
      ]}
    >
      {refresh.error || readings.error ? (
        <p role="alert">Could not refresh component health. Displayed readings and issues may be out of date.</p>
      ) : null}
      {detail ? (
        <ComponentStatusModalContent {...detail} />
      ) : (
        <StatusModalLayout
          icon={
            <DialogIcon
              intent={
                summary?.needsAttention
                  ? "critical"
                  : summary?.title === "All components operational"
                    ? "success"
                    : "info"
              }
            >
              {summary?.needsAttention ? (
                <Alert className="text-intent-critical-fill" />
              ) : summary?.title === "All components operational" ? (
                <Success />
              ) : (
                <Info />
              )}
            </DialogIcon>
          }
          title={isSleeping ? "Container is asleep" : (summary?.title ?? "Status unavailable")}
          subtitle={
            isSleeping
              ? "Wake your container to start hashing again"
              : (summary?.subtitle ?? "This location is no longer in the container map")
          }
          secondaryTitle={isSleeping && summary?.needsAttention ? summary.title : undefined}
          secondarySubtitle={isSleeping && summary?.needsAttention ? summary.subtitle : undefined}
          errors={rows.map((row, index) => ({
            key: `${row.componentName}:${row.message}:${index}`,
            title: row.message,
            subtitle: row.componentName,
            icon: <Alert className="text-intent-critical-fill" />,
            onClick: row.onClick,
          }))}
        >
          <dl className="grid grid-cols-2 gap-x-4 gap-y-6">
            {metrics.map((metric) => (
              <div key={metric.label}>
                <dt className="text-300 text-text-primary-50">{metric.label}</dt>
                <dd className="text-heading-200 text-text-primary">{metric.value}</dd>
              </div>
            ))}
          </dl>
          <p>Mining status: {miningStatusLabels[rollup?.miningStatus ?? "unknown"]}</p>
        </StatusModalLayout>
      )}
    </Modal>
  );
};

// Keep wake/auth state mounted when the summary closes to show confirmation.
const ContainerWakeFlow = (props: ProtoOSStatusModalProps) => {
  const { wakeMiner, shouldWake } = useWakeMiner();
  const isSleeping = useIsSleeping();
  if (shouldWake) return <WakingDialog open />;
  if (props.open === false) return null;
  return (
    <ContainerHealthModal
      {...props}
      isSleeping={isSleeping}
      onWake={
        isSleeping
          ? () => {
              props.onClose();
              wakeMiner();
            }
          : undefined
      }
    />
  );
};

const ContainerStatusFlow = (props: ProtoOSStatusModalProps) => {
  if (props.componentAddress || (props.scope && props.scope.kind !== "container")) {
    return props.open === false ? null : <ContainerHealthModal {...props} />;
  }
  return <ContainerWakeFlow {...props} />;
};

export default ContainerStatusFlow;
