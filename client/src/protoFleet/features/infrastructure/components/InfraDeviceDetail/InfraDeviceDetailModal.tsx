import { useCallback, useMemo, useState } from "react";

import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { Alert, Info, Success } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import { DialogIcon } from "@/shared/components/Dialog";
import Divider from "@/shared/components/Divider";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import Select from "@/shared/components/Select";
import StatusCircle from "@/shared/components/StatusCircle";
import Switch from "@/shared/components/Switch";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const buildOptions = (values: string[], currentValue: string) =>
  [...new Set([currentValue, ...values].filter(Boolean))].sort().map((value) => ({ value, label: value }));

const statusToCircle = (status: InfraDeviceItem["status"]) => {
  switch (status) {
    case "running":
      return "normal" as const;
    case "faulted":
      return "error" as const;
    case "unknown":
      return "warning" as const;
    default:
      return "inactive" as const;
  }
};

const formatLabel = (value: string | null) => (value ? value.replaceAll("_", " ") : "None");

const formatDeviceType = (device: InfraDeviceItem) => {
  if (device.endpointKind === "single_fan") return "Fan";
  if (device.fanCount && device.fanCount > 1) return `Fan group (${device.fanCount} fans)`;
  if (device.endpointKind === "fan_group") return "Fan group";
  return "";
};

interface InfraDeviceDetailModalProps {
  device: InfraDeviceItem;
  siteOptions?: string[];
  buildingOptions?: string[];
  onDismiss: () => void;
}

const InfraDeviceDetailModal = ({
  device,
  siteOptions = [],
  buildingOptions = [],
  onDismiss,
}: InfraDeviceDetailModalProps) => {
  const siteSelectOptions = useMemo(() => buildOptions(siteOptions, device.siteName), [siteOptions, device.siteName]);
  const buildingSelectOptions = useMemo(
    () => buildOptions(buildingOptions, device.buildingName),
    [buildingOptions, device.buildingName],
  );
  const [name, setName] = useState(device.name);
  const [endpoint, setEndpoint] = useState(device.endpoint);
  const [port, setPort] = useState(String(device.port));
  const [site, setSite] = useState(device.siteName);
  const [building, setBuilding] = useState(device.buildingName);
  const [enabled, setEnabled] = useState(device.enabled);
  const [isTesting, setIsTesting] = useState(false);

  const handleSave = useCallback(() => {
    onDismiss();
  }, [onDismiss]);

  const handleDelete = useCallback(() => {
    onDismiss();
  }, [onDismiss]);

  const handleTest = useCallback(() => {
    setIsTesting(true);
    setTimeout(() => {
      setIsTesting(false);
      pushToast({ message: `${device.name} endpoint connection successful (12ms)`, status: STATUSES.success });
    }, 1200);
  }, [device.name]);

  const hasUnackedIssue =
    device.issueStatus === "pending" || device.issueStatus === "failed" || device.issueStatus === "timed_out";

  const statusIcon = (() => {
    if (device.status === "unknown" || device.status === "stopped")
      return (
        <DialogIcon>
          <Info className="text-text-primary" />
        </DialogIcon>
      );
    if (device.status === "faulted" || hasUnackedIssue)
      return (
        <DialogIcon intent="critical">
          <Alert />
        </DialogIcon>
      );
    return (
      <DialogIcon intent="success">
        <Success />
      </DialogIcon>
    );
  })();

  const statusLabel = formatLabel(device.status);
  const description = formatDeviceType(device);

  return (
    <Modal
      open
      onDismiss={onDismiss}
      headerSpacingClassName="mt-6"
      buttons={[
        {
          text: "Delete",
          variant: variants.secondaryDanger,
          onClick: handleDelete,
          dismissModalOnClick: false,
        },
        {
          text: "Test connection",
          variant: variants.secondary,
          onClick: handleTest,
          loading: isTesting,
          dismissModalOnClick: false,
        },
        {
          text: "Save",
          variant: variants.primary,
          onClick: handleSave,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex flex-col gap-6">
        <div className="flex flex-col gap-3">
          {statusIcon}
          <div className="flex flex-col gap-1">
            <div className="text-heading-300 text-text-primary">{device.name}</div>
            {description ? <div className="text-300 text-text-primary-70">{description}</div> : null}
          </div>
        </div>

        {/* Editable fields */}
        <div className="flex flex-col gap-4">
          <Input id="device-name" label="Name" initValue={name} onChange={(v) => setName(v)} />
          <div className="grid grid-cols-2 gap-3">
            <Select
              id="device-site"
              label="Site"
              options={siteSelectOptions}
              value={site}
              onChange={setSite}
              forceBelow
            />
            <Select
              id="device-building"
              label="Building"
              options={buildingSelectOptions}
              value={building}
              onChange={setBuilding}
              forceBelow
            />
          </div>
          <div className="grid grid-cols-[1fr_160px] gap-3">
            <Input
              id="device-endpoint"
              label="Endpoint"
              initValue={endpoint}
              onChange={(v) => setEndpoint(v)}
            />
            <Input
              id="device-port"
              label="Port"
              type="number"
              inputMode="numeric"
              initValue={port}
              onChange={(v) => setPort(v)}
            />
          </div>
        </div>

        <Divider />

        {/* Device info */}
        <div className="flex flex-col">
          <Row compact>
            <div className="flex w-full items-center justify-between">
              <span className="text-text-primary-70">Enabled</span>
              <Switch
                checked={enabled === "auto"}
                setChecked={(next) => {
                  const checked = typeof next === "function" ? next(enabled === "auto") : next;
                  setEnabled(checked ? "auto" : "off");
                }}
              />
            </div>
          </Row>
          <Row compact>
            <div className="flex w-full items-center justify-between">
              <span className="text-text-primary-70">Status</span>
              <span className="flex items-center gap-2 capitalize">
                <StatusCircle status={statusToCircle(device.status)} variant="simple" width="w-[6px]" />
                {statusLabel}
              </span>
            </div>
          </Row>
          <Row compact>
            <div className="flex w-full items-center justify-between">
              <span className="text-text-primary-70">Issues</span>
              <span className="capitalize">{formatLabel(device.issueStatus)}</span>
            </div>
          </Row>
          <Row compact>
            <div className="flex w-full items-center justify-between">
              <span className="text-text-primary-70">Last seen</span>
              <span>{device.lastSeen}</span>
            </div>
          </Row>
          <Row compact divider={false}>
            <div className="flex w-full items-center justify-between">
              <span className="text-text-primary-70">Fans</span>
              <span>{device.fanCount ?? "—"}</span>
            </div>
          </Row>
        </div>
      </div>
    </Modal>
  );
};

export default InfraDeviceDetailModal;
