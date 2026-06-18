import { useCallback, useMemo, useState } from "react";

import { mockInfraDevices } from "@/protoFleet/features/maintenance/mockData";
import { Alert, Info, Success } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import { DialogIcon } from "@/shared/components/Dialog";
import Divider from "@/shared/components/Divider";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import Select from "@/shared/components/Select";
import StatusCircle from "@/shared/components/StatusCircle";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const SITE_OPTIONS = [
  { value: "Austin", label: "Austin" },
  { value: "Denver", label: "Denver" },
  { value: "Miami", label: "Miami" },
  { value: "Marfa", label: "Marfa" },
];

const BUILDING_OPTIONS = [...new Set(mockInfraDevices.map((d) => d.buildingName))].sort().map((b) => ({ value: b, label: b }));

interface InfraDeviceDetailModalProps {
  deviceId: string;
  onDismiss: () => void;
}

const statusToCircle = (status: string) => {
  switch (status) {
    case "online":
      return "normal" as const;
    case "degraded":
      return "warning" as const;
    default:
      return "inactive" as const;
  }
};

const InfraDeviceDetailModal = ({ deviceId, onDismiss }: InfraDeviceDetailModalProps) => {
  const device = useMemo(() => mockInfraDevices.find((d) => d.id === deviceId), [deviceId]);

  const [name, setName] = useState(device?.name ?? "");
  const [ipAddress, setIpAddress] = useState(device?.ipAddress ?? "");
  const [site, setSite] = useState(device?.siteName ?? "");
  const [building, setBuilding] = useState(device?.buildingName ?? "");
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
      pushToast({ message: `${device?.name ?? "Device"} — connection successful (12ms)`, status: STATUSES.success });
    }, 1200);
  }, [device?.name]);

  if (!device) return null;

  const protocol = device.deviceType === "fan" ? "Modbus" : "SNMP";
  const hasIssues = device.status === "degraded" || device.status === "offline" || device.issues > 0;

  const statusIcon = (() => {
    if (device.status === "offline") return <DialogIcon intent="info"><Info /></DialogIcon>;
    if (hasIssues) return <DialogIcon intent="critical"><Alert /></DialogIcon>;
    return <DialogIcon intent="success"><Success /></DialogIcon>;
  })();

  const statusTitle = (() => {
    if (device.status === "offline") return "Offline";
    if (device.status === "degraded") return "Degraded";
    return "Online";
  })();

  const statusSubtitle = (() => {
    if (device.status === "offline") return "Connection lost";
    if (device.status === "degraded") return "Degraded performance detected";
    if (device.issues > 0) return `${device.issues} active issue${device.issues !== 1 ? "s" : ""}`;
    return `${device.subtype}, ${device.model}`;
  })();

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title={<>{statusIcon}<div className="mt-3">{device.name}</div></>}
      description={statusSubtitle}
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
        {/* Editable fields */}
        <div className="flex flex-col gap-4">
          <Input id="device-name" label="Device name" initValue={name} onChange={(v) => setName(v)} />
          <Input id="device-ip" label="IP address" initValue={ipAddress} onChange={(v) => setIpAddress(v)} />
          <div className="grid grid-cols-2 gap-3">
            <Select id="device-site" label="Site" options={SITE_OPTIONS} value={site} onChange={setSite} forceBelow />
            <Select id="device-building" label="Building" options={BUILDING_OPTIONS} value={building} onChange={setBuilding} forceBelow />
          </div>
        </div>

        <Divider />

        {/* Device info */}
        <div className="flex flex-col">
          {device.firmware && (
            <Row compact>
              <div className="flex w-full items-center justify-between">
                <span className="text-text-primary-70">Firmware</span>
                <span>{device.firmware}</span>
              </div>
            </Row>
          )}
          {device.rpm != null && (
            <Row compact>
              <div className="flex w-full items-center justify-between">
                <span className="text-text-primary-70">Reading</span>
                <span>{device.rpm.toLocaleString()} RPM</span>
              </div>
            </Row>
          )}
          {device.powerW != null && (
            <Row compact>
              <div className="flex w-full items-center justify-between">
                <span className="text-text-primary-70">Power</span>
                <span>{device.powerW} W</span>
              </div>
            </Row>
          )}
          {device.temperatureC != null && (
            <Row compact>
              <div className="flex w-full items-center justify-between">
                <span className="text-text-primary-70">Temperature</span>
                <span>{device.temperatureC}°C</span>
              </div>
            </Row>
          )}
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
