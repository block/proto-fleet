// UnauthenticatedMiner represents the data structure used in the authentication flow
// It contains basic miner info along with credential fields
export type UnauthenticatedMiner = {
  deviceIdentifier: string;
  model: string;
  macAddress: string;
  ipAddress: string;
  username: string;
  password: string;
};

export type Credentials = {
  username: string;
  password: string;
};
