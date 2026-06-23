import { useState } from "react";
import { mockInfraDevices } from "../stories/mockInfraDevices";
import InfraDeviceDetailModal from "./InfraDeviceDetailModal";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import Button, { variants } from "@/shared/components/Button";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

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

export const Running = () => {
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
          onDismiss={() => setOpen(false)}
        />
      ) : null}
      <ToasterComponent />
    </>
  );
};

export const Faulted = () => {
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
          onDismiss={() => setOpen(false)}
        />
      ) : null}
      <ToasterComponent />
    </>
  );
};

export const Stopped = () => {
  const [open, setOpen] = useState(true);
  const device = findDevice("aus-b2-bridge-07");
  return (
    <>
      <Button variant={variants.primary} text="Open Modal" onClick={() => setOpen(true)} />
      {open ? (
        <InfraDeviceDetailModal
          device={device}
          siteOptions={siteOptions}
          buildingOptions={buildingOptions}
          onDismiss={() => setOpen(false)}
        />
      ) : null}
      <ToasterComponent />
    </>
  );
};
