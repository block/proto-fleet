import { useState } from "react";

import { mockInfraDevices } from "../stories/mockInfraDevices";
import AddInfraDeviceModal from "./AddInfraDeviceModal";
import Button, { variants } from "@/shared/components/Button";

export default {
  title: "Proto Fleet/Infrastructure/AddInfraDeviceModal",
  component: AddInfraDeviceModal,
};

const uniqueSorted = (values: string[]) => [...new Set(values.filter(Boolean))].sort();
const siteOptions = uniqueSorted(mockInfraDevices.map((device) => device.siteName));
const buildingOptions = uniqueSorted(mockInfraDevices.map((device) => device.buildingName));
const buildingOptionsBySite = mockInfraDevices.reduce<Record<string, string[]>>((acc, device) => {
  const siteBuildings = acc[device.siteName] ?? [];
  if (!siteBuildings.includes(device.buildingName)) {
    acc[device.siteName] = [...siteBuildings, device.buildingName].sort();
  }
  return acc;
}, {});

export const Default = () => {
  const [open, setOpen] = useState(true);
  return (
    <>
      <Button variant={variants.primary} text="Open Modal" onClick={() => setOpen(true)} />
      {open ? (
        <AddInfraDeviceModal
          siteOptions={siteOptions}
          buildingOptions={buildingOptions}
          buildingOptionsBySite={buildingOptionsBySite}
          onDismiss={() => setOpen(false)}
          onSuccess={() => setOpen(false)}
        />
      ) : null}
    </>
  );
};
