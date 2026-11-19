const parsePoolURL = (value: string) => {
  const url = value.trim();
  try {
    return new URL(url.includes("://") ? url : `stratum+tcp://${url}`);
  } catch {
    return undefined;
  }
};

const invalidAddressParts = (url: URL) =>
  !url.hostname || Boolean(url.username || url.password || url.search || url.hash || !["", "/"].includes(url.pathname));

// CT1's legacy ParsePoolURL also accepts host:port shorthand and DATUM gateways.
export const validateContainerPoolURL = (value: string): string | undefined => {
  const parsed = parsePoolURL(value);
  if (!parsed || invalidAddressParts(parsed)) return "Enter a valid pool host or IP address.";
  if (!["stratum+tcp:", "stratum+ssl:", "tcp:", "ssl:", "datum:"].includes(parsed.protocol))
    return "Use stratum+tcp://, stratum+ssl://, tcp://, ssl://, datum://, or host:port.";
};

export const validateMiningGroupPoolURL = (value: string): string | undefined => {
  const parsed = parsePoolURL(value);
  if (!parsed || invalidAddressParts(parsed) || /[?#]/.test(value)) return "Enter a valid pool host or IP address.";
  if (!["stratum+tcp:", "tcp:"].includes(parsed.protocol))
    return "Use stratum+tcp://, tcp://, or host:port for this configuration.";
};
