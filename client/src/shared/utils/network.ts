export const cidrToSubnetMask = (cidr: string) => {
  const maskBits = parseInt(cidr.split("/")[1], 10);
  if (isNaN(maskBits)) {
    // input not in CIDR notation
    return null;
  }

  // start with full mask and remove bits from the left, in the end use unsigned right shift (>>>) to treat number as unsigned
  const mask = (0xffffffff << (32 - maskBits)) >>> 0;
  // extract 8 bits at a time and combine the four octets into a string
  return [(mask >>> 24) & 0xff, (mask >>> 16) & 0xff, (mask >>> 8) & 0xff, mask & 0xff].join(".");
};
