import { useState } from "react";
import { mockInfraDevices } from "../stories/mockInfraDevices";
import InfraDeviceDetailModal from "./InfraDeviceDetailModal";
import {
  infraBuildingOptionsFromDevices,
  uniqueSortedLocationNames,
} from "@/protoFleet/features/infrastructure/locationOptions";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import Button, { variants } from "@/shared/components/Button";

export default {
  title: "Proto Fleet/Infrastructure/InfraDeviceDetailModal",
  component: InfraDeviceDetailModal,
};

const siteOptions = uniqueSortedLocationNames(mockInfraDevices.map((device) => device.siteName));
const buildingOptions = infraBuildingOptionsFromDevices(mockInfraDevices);
const findDevice = (id: string): InfraDeviceItem => {
  const device = mockInfraDevices.find((candidate) => candidate.id === id);
  if (!device) throw new Error(`Missing infrastructure device story fixture: ${id}`);
  return device;
};

export const Editable = () => {
  const [open, setOpen] = useState(true);
  const device = findDevice("101");
  return (
    <>
      <Button variant={variants.primary} text="Open Modal" onClick={() => setOpen(true)} />
      {open ? (
        <InfraDeviceDetailModal
          device={device}
          siteOptions={siteOptions}
          buildingOptions={buildingOptions}
          onSave={async () => setOpen(false)}
          onDelete={async () => setOpen(false)}
          onDismiss={() => setOpen(false)}
        />
      ) : null}
    </>
  );
};

export const RedactedConnection = () => {
  const [open, setOpen] = useState(true);
  const device = findDevice("108");
  return (
    <>
      <Button variant={variants.primary} text="Open Modal" onClick={() => setOpen(true)} />
      {open ? (
        <InfraDeviceDetailModal
          device={device}
          siteOptions={siteOptions}
          buildingOptions={buildingOptions}
          canManage={false}
          onSave={async () => setOpen(false)}
          onDelete={async () => setOpen(false)}
          onDismiss={() => setOpen(false)}
        />
      ) : null}
    </>
  );
};
