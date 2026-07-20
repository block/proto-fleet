export const maxFacilityFanDeviceSelections = 8;

export function selectAllFacilityFanDeviceIds(
  selectedDeviceIds: Iterable<string>,
  selectableDeviceIds: Iterable<string>,
): Set<string> {
  const selection = new Set(selectedDeviceIds);
  for (const deviceId of selectableDeviceIds) {
    if (selection.size >= maxFacilityFanDeviceSelections) {
      break;
    }
    selection.add(deviceId);
  }
  return selection;
}
