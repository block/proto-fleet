import { API_PROXY_BASE } from "@/protoFleet/api/constants";

const INSECURE_TRANSPORT_FLAG = "--allow-insecure-transport";
const HTTP_FLEET_API_PORT = "4000";

const isLoopbackHostname = (hostname: string) => {
  const normalized = hostname.toLowerCase().replace(/^\[|\]$/g, "");
  return normalized === "localhost" || normalized === "::1" || /^127(?:\.\d{1,3}){3}$/.test(normalized);
};

export const shouldAppendInsecureTransportFlag = (location: Pick<Location, "protocol" | "hostname">) =>
  location.protocol === "http:" && !isLoopbackHostname(location.hostname);

const getHttpFleetApiOrigin = (origin: string) => {
  const url = new URL(origin);
  url.port = HTTP_FLEET_API_PORT;
  return url.origin;
};

export const buildFleetNodeEnrollCommand = (location: Pick<Location, "origin" | "protocol" | "hostname">) => {
  const serverUrl =
    location.protocol === "http:" ? getHttpFleetApiOrigin(location.origin) : `${location.origin}${API_PROXY_BASE}`;
  const command = `fleetnode enroll --server-url=${serverUrl}`;
  return shouldAppendInsecureTransportFlag(location) ? `${command} ${INSECURE_TRANSPORT_FLAG}` : command;
};
