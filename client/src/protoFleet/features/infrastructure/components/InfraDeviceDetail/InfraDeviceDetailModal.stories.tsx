import { useState } from "react";
import { mockInfraDevices } from "../stories/mockInfraDevices";
import InfraDeviceDetailModal from "./InfraDeviceDetailModal";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import Button, { variants } from "@/shared/components/Button";

export default {
  title: "Proto Fleet/Infrastructure/InfraDeviceDetailModal",
  component: InfraDeviceDetailModal,
};

const siteOptions = [...new Set(mockInfraDevices.map((device) => device.siteName))].sort();
const buildingOptions = [...new Set(mockInfraDevices.map((device) => device.buildingName))].sort();
const findDevice = (id: string): InfraDeviceItem => {
  const device = mockInfraDevices.find((candidate) => candidate.id === id);
  if (!device) throw new Error(`Missing infrastructure device story fixture: ${id}`);
  return device;
};

export const Online = () => {
  const [open, setOpen] = useState(true);
  const device = findDevice("aus-b1-plc-01-zone-a");
  return (
    <>
      <Button variant={variants.primary} text="Open Modal" onClick={() => setOpen(true)} />
      {open ? (
        <InfraDeviceDetailModal
          device={device}
          siteOptions={siteOptions}
          buildingOptions={buildingOptions}
          onSave={() => setOpen(false)}
          onDelete={() => setOpen(false)}
          onDismiss={() => setOpen(false)}
        />
      ) : null}
    </>
  );
};

export const Offline = () => {
  const [open, setOpen] = useState(true);
  const device = findDevice("aus-b1-plc-01-zone-b");
  return (
    <>
      <Button variant={variants.primary} text="Open Modal" onClick={() => setOpen(true)} />
      {open ? (
        <InfraDeviceDetailModal
          device={device}
          siteOptions={siteOptions}
          buildingOptions={buildingOptions}
          onSave={() => setOpen(false)}
          onDelete={() => setOpen(false)}
          onDismiss={() => setOpen(false)}
        />
      ) : null}
    </>
  );
};
