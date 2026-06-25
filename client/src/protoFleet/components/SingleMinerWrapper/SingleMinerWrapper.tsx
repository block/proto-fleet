import { ReactNode, useEffect, useRef } from "react";
import { Link, useLocation, useParams } from "react-router-dom";
import type { SingleMinerMetadata, SingleMinerRouteState } from "./routeState";
import { singleMinerRoutePrefetch } from "@/protoFleet/routePrefetch";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
// eslint-disable-next-line no-restricted-imports -- Fleet shell hosts the protoOS single-miner experience
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { DismissCircleDark } from "@/shared/assets/icons";
import { prefetchRoutes } from "@/shared/utils/prefetchRoutes";

const CloseButton = ({ id }: { id: string }) => {
  const activeSite = useFleetStore((state) => state.ui.activeSite);
  return (
    <Link
      className="flex flex-row items-center gap-1 pl-2 text-300 text-text-primary-70"
      to={scopedPath("/fleet/miners", activeSite)}
    >
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

const routeMetadata = (state: unknown): SingleMinerMetadata | undefined =>
  (state as SingleMinerRouteState | null)?.singleMinerMetadata;

const SingleMinerWrapper = ({ children }: { children: ReactNode }) => {
  const { id: rawId } = useParams();
  const location = useLocation();
  const safeId = safePathSegment(rawId || "");
  const displayId = rawId || "";
  const currentRouteMetadata = routeMetadata(location.state);
  const metadataCacheRef = useRef<{ id: string; metadata?: SingleMinerMetadata }>({
    id: displayId,
    metadata: currentRouteMetadata,
  });

  if (metadataCacheRef.current.id !== displayId) {
    metadataCacheRef.current = { id: displayId, metadata: currentRouteMetadata };
  } else if (currentRouteMetadata) {
    metadataCacheRef.current.metadata = currentRouteMetadata;
  }

  const metadata = {
    minerName: metadataCacheRef.current.metadata?.minerName ?? displayId,
    ipAddress: metadataCacheRef.current.metadata?.ipAddress,
    macAddress: metadataCacheRef.current.metadata?.macAddress,
    firmwareVersion: metadataCacheRef.current.metadata?.firmwareVersion,
  };

  // Once the user is in /miners/:id/*, sibling protoOS chunks (KPI
  // tabs, Logs, Diagnostics, per-miner Settings) are one click away;
  // warm them at idle so tab switches have no Suspense flash.
  useEffect(() => {
    return prefetchRoutes(singleMinerRoutePrefetch);
  }, []);

  return (
    <MinerHostingProvider
      baseUrl={`/api-proxy/miners/${safeId}`}
      minerRoot={`/miners/${safeId}`}
      closeButton={(<CloseButton id={displayId} />) as ReactNode}
      mode="fleet"
      metadata={metadata}
    >
      {children}
    </MinerHostingProvider>
  );
};

export default SingleMinerWrapper;
