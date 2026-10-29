import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { isDefinitivePairRejection } from "@/protoFleet/api/fleetNodePairingErrors";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type {
  DevicePairingResult,
  FleetNodeDiscoveredDevice,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { DeviceSchema, DiscoverRequestSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import Miners from "@/protoFleet/features/onboarding/components/Miners/Miners";
import Button, { variants } from "@/shared/components/Button";

const DISCOVERED_PAGE_SIZE = 100;
const MAX_PAIRING_SELECTION = 1024;

interface FindNodeMinersModalProps {
  node: FleetNodeItem;
  canPair: boolean;
  blockedPairingIdentifiers: string[];
  settledPairingRevision: number;
  onDismiss: () => void;
  onUpdated: () => void;
  onPairingStarted: (identifiers: string[]) => void;
  onPairingCompleted: (identifiers: string[]) => void;
  onPairingSettledAfterDismiss: () => void;
  scanOnNode: ReturnType<typeof useFleetNodes>["discoverOnFleetNode"];
}

const FindNodeMinersModal = ({
  node,
  canPair,
  blockedPairingIdentifiers,
  settledPairingRevision,
  onDismiss,
  onUpdated,
  onPairingStarted,
  onPairingCompleted,
  onPairingSettledAfterDismiss,
  scanOnNode,
}: FindNodeMinersModalProps) => {
  const { listFleetNodeDiscoveredDevices, pairDiscoveredDevicesOnFleetNode } = useFleetNodes();
  const [discovered, setDiscovered] = useState<FleetNodeDiscoveredDevice[]>([]);
  const [nextCursor, setNextCursor] = useState(0n);
  const [pairResults, setPairResults] = useState<DevicePairingResult[]>([]);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [isScanning, setIsScanning] = useState(false);
  const [isPairing, setIsPairing] = useState(false);
  const [error, setError] = useState("");
  const [warning, setWarning] = useState("");
  const discoveredGenerationRef = useRef(0);
  const mountedRef = useRef(true);
  const loadedPagesRef = useRef(1);
  const lastSettledPairingRevisionRef = useRef(settledPairingRevision);
  const blockedIdentifiers = useMemo(() => new Set(blockedPairingIdentifiers), [blockedPairingIdentifiers]);
  const foundMiners = useMemo(
    () =>
      discovered
        .filter((device) => !blockedIdentifiers.has(device.deviceIdentifier))
        .map((device) =>
          create(DeviceSchema, {
            deviceIdentifier: device.deviceIdentifier,
            fleetNodeId: BigInt(node.fleetNodeId),
            boundToFleetNode: device.boundToFleetNode,
            ipAddress: device.ipAddress,
            port: device.port,
            urlScheme: device.urlScheme,
            driverName: device.driverName,
            model: device.model,
            manufacturer: device.manufacturer,
            firmwareVersion: device.firmwareVersion,
            deviceClass: device.deviceClass,
          }),
        ),
    [blockedIdentifiers, discovered, node.fleetNodeId],
  );

  const refresh = useCallback(
    async (preservePages = false) => {
      const generation = ++discoveredGenerationRef.current;
      setIsLoadingMore(false);
      try {
        const devices: FleetNodeDiscoveredDevice[] = [];
        let cursor = 0n;
        let loadedPages = 0;
        const pagesToLoad = preservePages ? loadedPagesRef.current : 1;
        do {
          const page = await listFleetNodeDiscoveredDevices(node.fleetNodeId, cursor);
          if (generation !== discoveredGenerationRef.current || !mountedRef.current) return false;
          devices.push(...page.devices);
          cursor = page.nextCursor;
          loadedPages++;
        } while (cursor !== 0n && loadedPages < pagesToLoad);
        setDiscovered(devices);
        setNextCursor(cursor);
        loadedPagesRef.current = loadedPages;
        return true;
      } catch (err) {
        if (generation === discoveredGenerationRef.current && mountedRef.current) throw err;
        return false;
      }
    },
    [listFleetNodeDiscoveredDevices, node.fleetNodeId],
  );

  const loadMore = async () => {
    if (nextCursor === 0n || isLoadingMore || isScanning || isPairing) return;
    const generation = discoveredGenerationRef.current;
    setIsLoadingMore(true);
    try {
      const next = await listFleetNodeDiscoveredDevices(node.fleetNodeId, nextCursor);
      if (generation !== discoveredGenerationRef.current) return;
      setDiscovered((current) => [...current, ...next.devices]);
      setNextCursor(next.nextCursor);
      loadedPagesRef.current++;
    } catch (err) {
      if (generation === discoveredGenerationRef.current) {
        setError(err instanceof Error ? err.message : "Failed to load more miners.");
      }
    } finally {
      if (generation === discoveredGenerationRef.current) setIsLoadingMore(false);
    }
  };

  const scan = useCallback(async () => {
    setError("");
    setWarning("");
    setIsScanning(true);
    let scanFailed = false;
    try {
      await scanOnNode(
        node.fleetNodeId,
        create(DiscoverRequestSchema, { mode: { case: "networkScan", value: { useFleetNodeLocalSubnet: true } } }),
        (response) => {
          if (!mountedRef.current) return;
          if (response.warning) setWarning(response.warning);
        },
      );
    } catch (err) {
      scanFailed = true;
      if (mountedRef.current) setError(err instanceof Error ? err.message : "Discovery failed.");
    } finally {
      if (mountedRef.current) {
        try {
          await refresh();
          if (mountedRef.current && !scanFailed) setError("");
        } catch (err) {
          if (mountedRef.current && !scanFailed) {
            setError(err instanceof Error ? err.message : "Failed to load discovered miners.");
          }
        } finally {
          if (mountedRef.current) setIsScanning(false);
        }
      }
    }
  }, [scanOnNode, node.fleetNodeId, refresh]);

  useEffect(() => {
    let active = true;
    const generationRef = discoveredGenerationRef;
    mountedRef.current = true;
    void Promise.resolve().then(() => {
      if (!active) return;
      void refresh().catch((err) => {
        if (active) setError(err instanceof Error ? err.message : "Failed to load discovered miners.");
      });
      void scan();
    });
    return () => {
      active = false;
      mountedRef.current = false;
      generationRef.current++;
    };
  }, [refresh, scan]);

  useEffect(() => {
    if (settledPairingRevision === lastSettledPairingRevisionRef.current || isScanning || isPairing) return;
    void refresh(true)
      .then((applied) => {
        if (applied) lastSettledPairingRevisionRef.current = settledPairingRevision;
      })
      .catch((err) => {
        if (mountedRef.current) setError(err instanceof Error ? err.message : "Failed to load discovered miners.");
      });
  }, [settledPairingRevision, isScanning, isPairing, refresh]);

  const pairSelected = async (selected: string[]) => {
    if (
      !canPair ||
      selected.length === 0 ||
      selected.length > MAX_PAIRING_SELECTION ||
      selected.some((identifier) => blockedIdentifiers.has(identifier))
    )
      return;
    setError("");
    setPairResults([]);
    setIsPairing(true);
    const submitted = [...selected];
    onPairingStarted(submitted);
    const unreported = new Set(submitted);
    const paired = new Set<string>();
    let pairingCompleted = false;
    let receivedPairingOutcome = false;
    try {
      await pairDiscoveredDevicesOnFleetNode(node.fleetNodeId, submitted, undefined, (results) => {
        // FAILED can be a server-synthesized placeholder after a Node timeout
        // or disconnect, so it is not proof that retrying the miner is safe.
        if (results.some((result) => result.pairingStatus !== PairingStatus.FAILED)) receivedPairingOutcome = true;
        const completed = results
          .filter(
            (result) =>
              result.pairingStatus === PairingStatus.PAIRED ||
              result.pairingStatus === PairingStatus.DEFAULT_PASSWORD ||
              result.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED,
          )
          .map((result) => result.deviceIdentifier)
          .filter((identifier) => unreported.delete(identifier));
        if (completed.length > 0) onPairingCompleted(completed);
        if (mountedRef.current) {
          setPairResults((current) => [...current, ...results]);
        }
        for (const result of results) {
          if (result.pairingStatus === PairingStatus.PAIRED || result.pairingStatus === PairingStatus.DEFAULT_PASSWORD)
            paired.add(result.deviceIdentifier);
        }
        if (mountedRef.current && paired.size > 0)
          setDiscovered((current) => current.filter((device) => !paired.has(device.deviceIdentifier)));
      });
      pairingCompleted = true;
      if (mountedRef.current) await refresh();
      onUpdated();
      if (mountedRef.current) {
        if (paired.size === submitted.length) onDismiss();
        else setError("Some pairing results need attention. Check miner state before retrying.");
      }
    } catch (err) {
      // A disconnected result stream does not mean the Node stopped pairing.
      if (!pairingCompleted && !receivedPairingOutcome && isDefinitivePairRejection(err)) {
        onPairingCompleted([...unreported]);
      }
      if (mountedRef.current) {
        setError(
          err instanceof Error
            ? err.message
            : pairingCompleted
              ? "Paired miners, but failed to refresh their status."
              : "Pairing result is unknown. Check miner state before retrying.",
        );
      }
      try {
        if (mountedRef.current) await refresh();
        onUpdated();
      } catch {
        // Keep the original error visible.
      }
    } finally {
      if (mountedRef.current) setIsPairing(false);
      else onPairingSettledAfterDismiss();
    }
  };

  return (
    <div data-testid="find-node-miners-flow">
      <Miners
        mode="pairing"
        initialStep="pairing"
        foundMiners={foundMiners}
        fleetNodes={[node]}
        scanDiscoveryPending={isScanning}
        manualDiscoveryPending={false}
        pairingPending={isPairing}
        networkInfoPending={false}
        scanAvailable={Boolean(node.controlStreamConnected && !node.commandProtocolUpgradeRequired)}
        canAddMiners={Boolean(canPair && !isScanning && !isPairing)}
        allowDismissWhilePairing
        maxSelectedMiners={MAX_PAIRING_SELECTION}
        onCancelScan={onDismiss}
        onManualDiscover={() => undefined}
        onContinue={(identifiers) => void pairSelected(identifiers)}
        onRescan={() => void scan()}
        notice={
          <>
            {error ? (
              <div role="alert" className="mb-4 text-300 text-intent-critical-fill">
                {error}
              </div>
            ) : null}
            {warning ? (
              <div role="status" className="mb-4 text-300 text-intent-warning-fill">
                Discovery incomplete: {warning}
              </div>
            ) : null}
            {blockedPairingIdentifiers.length > 0 ? (
              <div role="status" className="mb-4 text-300 text-intent-warning-fill">
                {blockedPairingIdentifiers.length} miners have an unknown pairing result. Check their state before
                retrying; reload this page after checking to clear this block.
              </div>
            ) : null}
          </>
        }
      >
        {nextCursor !== 0n ? (
          <Button
            variant={variants.textOnly}
            text={"Load next " + DISCOVERED_PAGE_SIZE}
            loading={isLoadingMore}
            disabled={isScanning || isPairing}
            onClick={() => void loadMore()}
          />
        ) : null}
        {pairResults.length > 0 ? (
          <section className="mt-6">
            <h3 className="mb-2 text-heading-200">Pairing results</h3>
            <ul className="text-300" aria-label="Pairing results">
              {pairResults.map((result) => (
                <li key={result.deviceIdentifier}>
                  {result.deviceIdentifier}: {PairingStatus[result.pairingStatus] ?? "Unknown"}
                  {result.error ? " · " + result.error : ""}
                </li>
              ))}
            </ul>
          </section>
        ) : null}
      </Miners>
    </div>
  );
};

export default FindNodeMinersModal;
