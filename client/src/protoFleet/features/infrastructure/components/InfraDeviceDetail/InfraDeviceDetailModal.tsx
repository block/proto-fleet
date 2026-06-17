import { useCallback, useMemo, useState } from "react";

import { mockInfraDevices } from "@/protoFleet/features/maintenance/mockData";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import StatusCircle from "@/shared/components/StatusCircle";

const SITE_OPTIONS = [
  { value: "Austin", label: "Austin" },
  { value: "Denver", label: "Denver" },
  { value: "Miami", label: "Miami" },
  { value: "Marfa", label: "Marfa" },
];

const BUILDING_OPTIONS = [
  { value: "Building 1", label: "Building 1" },
  { value: "Building 2", label: "Building 2" },
  { value: "Building 3", label: "Building 3" },
  { value: "Building 4", label: "Building 4" },
  { value: "Building 5", label: "Building 5" },
  { value: "Building 10", label: "Building 10" },
];

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
  const [testResult, setTestResult] = useState<string | null>(null);

  const handleSave = useCallback(() => {
    onDismiss();
  }, [onDismiss]);

  const handleDelete = useCallback(() => {
    onDismiss();
  }, [onDismiss]);

  const handleTest = useCallback(() => {
    setIsTesting(true);
    setTestResult(null);
    setTimeout(() => {
      setIsTesting(false);
      setTestResult("Connection successful (12ms)");
    }, 1200);
  }, []);

  if (!device) return null;

  const protocol = device.deviceType === "fan" ? "Modbus" : "SNMP";
  const subtextParts = [
    device.subtype,
    `${device.buildingName}, ${device.siteName}`,
    device.rpm ? `${device.rpm.toLocaleString()} RPM` : null,
    protocol,
  ].filter(Boolean);

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title={device.name}
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
      <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <span className="text-300 text-text-primary-70">{subtextParts.join(", ")}</span>
          <div className="flex shrink-0 items-center gap-2">
            <StatusCircle status={statusToCircle(device.status)} />
            <span className="text-300 capitalize">{device.status}</span>
          </div>
        </div>

        <Input id="device-name" label="Device name" initValue={name} onChange={(v) => setName(v)} />
        <Input id="device-ip" label="IP address" initValue={ipAddress} onChange={(v) => setIpAddress(v)} />
        <div className="grid grid-cols-2 gap-3">
          <Select id="device-site" label="Site" options={SITE_OPTIONS} value={site} onChange={setSite} forceBelow />
          <Select id="device-building" label="Building" options={BUILDING_OPTIONS} value={building} onChange={setBuilding} forceBelow />
        </div>

        {testResult && (
          <div className="rounded-lg bg-intent-success-10 px-4 py-2 text-300 text-text-primary">
            {testResult}
          </div>
        )}
      </div>
    </Modal>
  );
};

export default InfraDeviceDetailModal;
