const MAC_ADDRESS = /^(?:[0-9a-f]{2}:){5}[0-9a-f]{2}$/i;

export const isMacAddress = (value: string) => MAC_ADDRESS.test(value);
