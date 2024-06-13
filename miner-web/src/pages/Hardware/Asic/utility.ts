export const getAsicUniqueId = (asicID: number, hashboardSerial: string) => {
  return `${hashboardSerial}-${asicID}`;
};
