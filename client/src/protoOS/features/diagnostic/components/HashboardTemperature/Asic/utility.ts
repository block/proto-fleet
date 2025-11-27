export const getAsicUniqueId = (asicID: string | number, hashboardSerial: string) => {
  return `${hashboardSerial}-${asicID}`;
};
