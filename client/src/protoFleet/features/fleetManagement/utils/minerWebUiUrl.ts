type MinerWebUiUrlInput = {
  ipAddress?: string;
  url?: string;
};

const formatHostForUrl = (host: string) => {
  if (host.includes(":") && !host.startsWith("[") && !host.endsWith("]")) {
    return `[${host}]`;
  }
  return host;
};

export const getMinerWebUiUrl = ({ ipAddress, url }: MinerWebUiUrlInput): string | undefined => {
  const trimmedIpAddress = ipAddress?.trim();
  if (trimmedIpAddress) {
    return `http://${formatHostForUrl(trimmedIpAddress)}`;
  }

  const trimmedUrl = url?.trim();
  if (!trimmedUrl) {
    return undefined;
  }

  try {
    const parsedUrl = new URL(trimmedUrl);
    return `http://${formatHostForUrl(parsedUrl.hostname)}`;
  } catch {
    return undefined;
  }
};
