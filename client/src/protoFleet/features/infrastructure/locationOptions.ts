import type { InfraBuildingOption, InfraDeviceItem, InfraRackOption } from "@/protoFleet/features/infrastructure/types";

export const uniqueSortedLocationNames = (values: string[]) => [...new Set(values.filter(Boolean))].sort();

export const uniqueInfraBuildingOptions = (options: InfraBuildingOption[]) => {
  const seen = new Set<string>();
  return options
    .filter((option) => option.siteName && option.buildingName)
    .sort((a, b) => a.siteName.localeCompare(b.siteName) || a.buildingName.localeCompare(b.buildingName))
    .filter((option) => {
      const key = `${option.siteName}\u0000${option.buildingName}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
};

export const infraBuildingOptionsFromDevices = (devices: InfraDeviceItem[]) =>
  uniqueInfraBuildingOptions(
    devices.map((device) => ({ siteName: device.siteName, buildingName: device.buildingName })),
  );

export const uniqueInfraRackOptions = (options: InfraRackOption[]) => {
  const seen = new Set<string>();
  return options
    .filter((option) => option.siteName && option.buildingName && option.rackName)
    .sort(
      (a, b) =>
        a.siteName.localeCompare(b.siteName) ||
        a.buildingName.localeCompare(b.buildingName) ||
        a.rackName.localeCompare(b.rackName),
    )
    .filter((option) => {
      const key = `${option.siteName}\u0000${option.buildingName}\u0000${option.rackName}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
};

export const infraRackOptionsFromDevices = (devices: InfraDeviceItem[]) =>
  uniqueInfraRackOptions(
    devices.map((device) => ({
      siteName: device.siteName,
      buildingName: device.buildingName,
      rackName: device.rackName,
    })),
  );
