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
import { pushToast, STATUSES } from "@/shared/features/toaster";

const buildOptions = (values: string[], currentValue: string) =>
  [...new Set([currentValue, ...values].filter(Boolean))].sort().map((value) => ({ value, label: value }));

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
  const [ipAddress, setIpAddress] = useState(device.ipAddress);
  const [site, setSite] = useState(device.siteName);
  const [building, setBuilding] = useState(device.buildingName);
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
      pushToast({ message: `${device.name} — connection successful (12ms)`, status: STATUSES.success });
    }, 1200);
  }, [device.name]);

  const protocol = device.deviceType === "fan" ? "Modbus" : "SNMP";
  const hasIssues = device.status === "degraded" || device.status === "offline" || device.issues > 0;

  const statusIcon = (() => {
    if (device.status === "offline")
      return (
        <DialogIcon intent="info">
          <Info />
        </DialogIcon>
      );
    if (hasIssues)
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

  const statusLabel = (() => {
    if (device.status === "offline") return "Connection lost";
    if (device.status === "degraded") return "Degraded performance";
    return "Online";
  })();

  const subtextParts = [device.subtype, device.model].filter(Boolean).join(", ");

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title={device.name}
      description={subtextParts}
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
        <div className="flex items-center justify-between gap-4">
          {statusIcon}
          <span className="flex items-center gap-1.5 text-text-primary">
            <StatusCircle
              status={device.status === "online" ? "normal" : device.status === "degraded" ? "warning" : "inactive"}
              variant="simple"
              width="w-[6px]"
            />
            {statusLabel}
          </span>
        </div>

        {/* Editable fields */}
        <div className="flex flex-col gap-4">
          <Input id="device-name" label="Device name" initValue={name} onChange={(v) => setName(v)} />
          <Input id="device-ip" label="IP address" initValue={ipAddress} onChange={(v) => setIpAddress(v)} />
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
        </div>

        <Divider />

        {/* Device info */}
        <div className="flex flex-col">
          {device.firmware ? (
            <Row compact>
              <div className="flex w-full items-center justify-between">
                <span className="text-text-primary-70">Firmware</span>
                <span>{device.firmware}</span>
              </div>
            </Row>
          ) : null}
          {device.rpm != null ? (
            <Row compact>
              <div className="flex w-full items-center justify-between">
                <span className="text-text-primary-70">Reading</span>
                <span>{device.rpm.toLocaleString()} RPM</span>
              </div>
            </Row>
          ) : null}
          {device.powerW != null ? (
            <Row compact>
              <div className="flex w-full items-center justify-between">
                <span className="text-text-primary-70">Power</span>
                <span>{device.powerW} W</span>
              </div>
            </Row>
          ) : null}
          {device.temperatureC != null ? (
            <Row compact>
              <div className="flex w-full items-center justify-between">
                <span className="text-text-primary-70">Temperature</span>
                <span>{device.temperatureC}°C</span>
              </div>
            </Row>
          ) : null}
          <Row compact>
            <div className="flex w-full items-center justify-between">
              <span className="text-text-primary-70">Last seen</span>
              <span>{device.lastSeen}</span>
            </div>
          </Row>
          <Row compact divider={false}>
            <div className="flex w-full items-center justify-between">
              <span className="text-text-primary-70">Protocol</span>
              <span>{protocol}</span>
            </div>
          </Row>
        </div>
      </div>
    </Modal>
  );
};

export default InfraDeviceDetailModal;
