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

export const Online = () => {
  const [open, setOpen] = useState(true);
  const device = findDevice("fan-den-1-1");
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

export const Degraded = () => {
  const [open, setOpen] = useState(true);
  const device = findDevice("fan-den-5-2");
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

export const Offline = () => {
  const [open, setOpen] = useState(true);
  const device = findDevice("fan-aus-1-3");
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
