// DeviceListItem represents a device in the miner list
// All devices (paired and unpaired) use the same MinerStateSnapshot type from the store
export type DeviceListItem = {
  deviceIdentifier: string;
};
