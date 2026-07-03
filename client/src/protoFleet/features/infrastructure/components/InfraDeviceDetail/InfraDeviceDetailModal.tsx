import { useCallback, useState } from "react";

import InfraLocationFields from "@/protoFleet/features/infrastructure/components/InfraLocationFields";
import {
  MODBUS_TCP_CONNECTION_TYPE,
  MODBUS_TCP_CONNECTION_TYPE_LABEL,
} from "@/protoFleet/features/infrastructure/connectionTypes";
import { FieldHelpPopover } from "@/protoFleet/features/infrastructure/fieldHelp";
import { infraDeviceFieldHelp } from "@/protoFleet/features/infrastructure/fieldHelpContent";
import type { InfraBuildingOption, InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { Alert, Success } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import { DialogIcon } from "@/shared/components/Dialog";
import Divider from "@/shared/components/Divider";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import StatusCircle from "@/shared/components/StatusCircle";
import Switch from "@/shared/components/Switch";

const statusToCircle = (status: InfraDeviceItem["status"]) => {
  switch (status) {
    case "online":
      return "normal" as const;
    case "offline":
      return "error" as const;
    default:
      return "inactive" as const;
  }
};

const formatStatus = (status: InfraDeviceItem["status"]) => (status === "online" ? "Online" : "Offline");

const formatDeviceType = (device: InfraDeviceItem) => {
  if (device.endpointKind === "single_fan") return "Fan";
  if (device.fanCount && device.fanCount > 1) return `Fan group (${device.fanCount} fans)`;
  if (device.endpointKind === "fan_group") return "Fan group";
  return "";
};

interface InfraDeviceDetailModalProps {
  device: InfraDeviceItem;
  siteOptions?: string[];
  buildingOptions?: InfraBuildingOption[];
  canManage?: boolean;
  onSave: (device: InfraDeviceItem) => void;
  onDelete: (deviceId: string) => void;
  onDismiss: () => void;
}

const InfraDeviceDetailModal = ({
  device,
  siteOptions = [],
  buildingOptions = [],
  canManage = true,
  onSave,
  onDelete,
  onDismiss,
}: InfraDeviceDetailModalProps) => {
  const [site, setSite] = useState(device.siteName);
  const [name, setName] = useState(device.name);
  const [endpoint, setEndpoint] = useState(device.endpoint);
  const [port, setPort] = useState(String(device.port));
  const [building, setBuilding] = useState(device.buildingName);
  const [enabled, setEnabled] = useState(device.enabled);
  const portNumber = Number(port);
  const isPortValid = Number.isInteger(portNumber) && portNumber > 0 && portNumber <= 65535;
  const canSave = [name, site, building, endpoint].every((value) => value.trim().length > 0) && isPortValid;

  const handleSave = useCallback(() => {
    if (!canSave) return;
    onSave({
      ...device,
      name: name.trim(),
      connectionType: MODBUS_TCP_CONNECTION_TYPE,
      endpoint: endpoint.trim(),
      port: portNumber,
      siteName: site.trim(),
      buildingName: building.trim(),
      enabled,
    });
    onDismiss();
  }, [building, canSave, device, enabled, endpoint, name, onDismiss, onSave, portNumber, site]);

  const handleDelete = useCallback(() => {
    onDelete(device.id);
  }, [device.id, onDelete]);

  const statusIcon = (() => {
    if (device.status === "offline")
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

  const statusLabel = formatStatus(device.status);
  const description = formatDeviceType(device);

  return (
    <Modal
      open
      onDismiss={onDismiss}
      headerSpacingClassName="mt-6"
      buttons={
        canManage
          ? [
              {
                text: "Delete",
                variant: variants.secondaryDanger,
                onClick: handleDelete,
                dismissModalOnClick: false,
              },
              {
                text: "Save",
                variant: variants.primary,
                onClick: handleSave,
                disabled: !canSave,
                dismissModalOnClick: false,
              },
            ]
          : []
      }
    >
      <div className="flex flex-col gap-6">
        <div className="flex flex-col gap-3">
          {statusIcon}
          <div className="flex flex-col gap-1">
            <div className="text-heading-300 text-text-primary">{device.name}</div>
            <div className="flex flex-col gap-1 text-300 text-text-primary-70">
              {description ? <span>{description}</span> : null}
              <span className="inline-flex items-center gap-1.5">
                <StatusCircle status={statusToCircle(device.status)} variant="simple" width="w-[6px]" removeMargin />
                {statusLabel}
              </span>
            </div>
          </div>
        </div>

        {/* Editable fields */}
        <div className="flex flex-col gap-4">
          <Input id="device-name" label="Name" initValue={name} readOnly={!canManage} onChange={(v) => setName(v)} />
          <InfraLocationFields
            site={site}
            building={building}
            siteOptions={siteOptions}
            buildingOptions={buildingOptions}
            onSiteChange={setSite}
            onBuildingChange={setBuilding}
            disabled={!canManage}
          />
          <Input
            id="device-connection-type"
            label="Connection type"
            initValue={MODBUS_TCP_CONNECTION_TYPE_LABEL}
            readOnly
            suffixAction={<FieldHelpPopover {...infraDeviceFieldHelp.connectionType} />}
          />
          <div className="grid grid-cols-2 gap-3">
            <Input
              id="device-endpoint"
              label="Endpoint"
              initValue={endpoint}
              readOnly={!canManage}
              suffixAction={<FieldHelpPopover {...infraDeviceFieldHelp.endpoint} />}
              onChange={(v) => setEndpoint(v)}
            />
            <Input
              id="device-port"
              label="Port"
              type="number"
              inputMode="numeric"
              initValue={port}
              readOnly={!canManage}
              suffixAction={<FieldHelpPopover {...infraDeviceFieldHelp.port} />}
              onChange={(v) => setPort(v)}
            />
          </div>
          <div className="flex h-14 items-center justify-between rounded-lg border border-border-5 bg-surface-base px-4 transition duration-200 ease-in-out">
            <span className="text-300 text-text-primary">Enabled</span>
            <Switch
              ariaLabel="Enabled"
              checked={enabled === "auto"}
              disabled={!canManage}
              setChecked={(next) => {
                const checked = typeof next === "function" ? next(enabled === "auto") : next;
                setEnabled(checked ? "auto" : "off");
              }}
            />
          </div>
        </div>

        <Divider />

        {/* Device info */}
        <div className="flex flex-col">
          <Row compact>
            <div className="flex w-full items-center justify-between gap-4">
              <span className="text-text-primary-70">Unit ID</span>
              <span className="truncate text-300 text-text-primary-70">{device.id}</span>
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
