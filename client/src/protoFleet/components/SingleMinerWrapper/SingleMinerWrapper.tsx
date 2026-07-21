import { motion } from "motion/react";
import { Fragment, ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { recallSingleMinerMetadata, type SingleMinerMetadata, type SingleMinerRouteState } from "./routeState";
import { singleMinerRoutePrefetch } from "@/protoFleet/routePrefetch";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
// eslint-disable-next-line no-restricted-imports -- Fleet shell hosts the protoOS single-miner experience
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { Dismiss } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import useSlideUpAnimation from "@/shared/hooks/useSlideUpAnimation";
import { prefetchRoutes } from "@/shared/utils/prefetchRoutes";

const CloseButton = ({ label, onClose }: { label: string; onClose: () => void }) => (
  <div className="flex min-w-0 items-center gap-3">
    <Button
      ariaLabel="Close miner view"
      variant={variants.secondary}
      size={sizes.base}
      prefixIcon={<Dismiss />}
      onClick={onClose}
      testId="single-miner-close-button"
    />
    <span className="truncate text-heading-100 text-text-primary">{label}</span>
  </div>
);

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
  const navigate = useNavigate();
  const activeSite = useFleetStore((state) => state.ui.activeSite);
  const slideUpAnimation = useSlideUpAnimation();
  const [isClosing, setIsClosing] = useState(false);
  const safeId = safePathSegment(rawId || "");
  const displayId = rawId || "";
  // location.state survives a direct render; the device-keyed cache survives the
  // protoOS loader redirects (which drop navigation state).
  const cachedMetadata = routeMetadata(location.state) ?? recallSingleMinerMetadata(displayId);

  // Stable identity: useMinerHosting memoizes on `metadata`, so a fresh object
  // every render would bust that memo for every embedded consumer.
  const metadata = useMemo(
    () => ({
      minerName: cachedMetadata?.minerName ?? displayId,
      ipAddress: cachedMetadata?.ipAddress,
      macAddress: cachedMetadata?.macAddress,
      firmwareVersion: cachedMetadata?.firmwareVersion,
      site: cachedMetadata?.site,
      building: cachedMetadata?.building,
      rack: cachedMetadata?.rack,
    }),
    [cachedMetadata, displayId],
  );

  const handleClose = useCallback(() => setIsClosing(true), []);
  // Guarantees the close navigation fires once even if the exit animation
  // replays (StrictMode double-mount, a second close click, or a back-nav that
  // re-enters this still-mounted layout route in the closing pose).
  const hasNavigatedOnClose = useRef(false);

  // Once the user is in /miners/:id/*, sibling protoOS chunks (KPI tabs, Logs,
  // Diagnostics, per-miner Settings) are one click away; warm them at idle so
  // tab switches have no Suspense flash.
  useEffect(() => {
    return prefetchRoutes(singleMinerRoutePrefetch);
  }, []);

  return (
    <MinerHostingProvider
      baseUrl={`/api-proxy/miners/${safeId}`}
      minerRoot={`/miners/${safeId}`}
      closeButton={<CloseButton label={metadata.minerName} onClose={handleClose} />}
      mode="fleet"
      metadata={metadata}
    >
      <div className="min-h-screen bg-surface-base text-text-primary" data-testid="single-miner-surface">
        {/* Mirror the full-screen modal: slide/fade in on open, then finish the
            exit animation before routing back to the miners list on close. The
            outer surface stays opaque while this content fades, avoiding a
            route-transition flash against the document background. Mounted on
            the parent route, so this plays once per visit (not per tab). */}
        <motion.div
          className="min-h-screen bg-surface-base"
          data-testid="single-miner-content"
          initial={slideUpAnimation.initial}
          animate={isClosing ? slideUpAnimation.exit : slideUpAnimation.animate}
          transition={slideUpAnimation.transition}
          onAnimationComplete={() => {
            if (isClosing && !hasNavigatedOnClose.current) {
              hasNavigatedOnClose.current = true;
              navigate(scopedPath("/fleet/miners", activeSite));
            }
          }}
        >
          {/* Key the hosted ProtoOS subtree by miner so a direct A->B route
              change (React Router reuses this element) remounts it: the store
              reset clears the shared slices, and this clears page-local state
              (e.g. Cooling's local mode, MiningPools' local pools) so B never
              inherits A's UI. */}
          <Fragment key={safeId}>{children}</Fragment>
        </motion.div>
      </div>
    </MinerHostingProvider>
  );
};

export default SingleMinerWrapper;
