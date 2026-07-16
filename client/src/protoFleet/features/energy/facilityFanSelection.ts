export const maxFacilityFanDeviceSelections = 1024;

export function selectAllFacilityFanDeviceIds(
  selectedDeviceIds: Iterable<string>,
  selectableDeviceIds: Iterable<string>,
): Set<string> {
  const selection = new Set([...selectedDeviceIds].slice(0, maxFacilityFanDeviceSelections));
  for (const deviceId of selectableDeviceIds) {
    if (selection.size >= maxFacilityFanDeviceSelections) {
      break;
    }
    selection.add(deviceId);
  }
  return selection;
}
