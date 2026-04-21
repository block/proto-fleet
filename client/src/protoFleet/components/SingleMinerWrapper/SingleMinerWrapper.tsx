import { ReactNode } from "react";
import { Link, useParams } from "react-router-dom";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { DismissCircleDark } from "@/shared/assets/icons";

const CloseButton = ({ id }: { id: string }) => {
  return (
    <Link className="flex flex-row items-center gap-1 pl-2 text-300 text-text-primary-70" to={"/miners"}>
      <DismissCircleDark />
      {id}
    </Link>
  );
};

/** Encode the route param as a single safe path segment. Strips C0 control
 *  characters and whitespace, then re-encodes so /, \, .., ?, # etc. are
 *  never interpreted as URL structure when used in baseUrl or minerRoot. */
// eslint-disable-next-line no-control-regex
const safePathSegment = (raw: string): string => encodeURIComponent(raw.replace(/[\x00-\x1f\x7f]/g, ""));

const SingleMinerWrapper = ({ children }: { children: ReactNode }) => {
  const { id: rawId } = useParams();
  const safeId = safePathSegment(rawId || "");
  const displayId = rawId || "";

  // Here we are just setting the base url to <vite_server>/:id,
  // which vite proxies to the actual miner api server.
  // If we wanted to make this request to ProtoFleet backend we
  // could pass <protofleet_host>/miners/:id instead
  return (
    <MinerHostingProvider
      baseUrl={safeId}
      minerRoot={`/miners/${safeId}`}
      closeButton={(<CloseButton id={displayId} />) as ReactNode}
    >
      {children}
    </MinerHostingProvider>
  );
};

export default SingleMinerWrapper;
