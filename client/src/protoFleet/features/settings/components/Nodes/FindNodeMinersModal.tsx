import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type {
  DevicePairingResult,
  FleetNodeDiscoveredDevice,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { CredentialsSchema, DiscoverRequestSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { getErrorCause } from "@/protoFleet/api/requestErrors";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";

const DISCOVERED_PAGE_SIZE = 100;
const MAX_PAIRING_SELECTION = 1024;

const isDefinitivePairRejection = (error: unknown): boolean => {
  const cause = getErrorCause(error);
  return (
    cause instanceof ConnectError &&
    (cause.code === Code.InvalidArgument ||
      (cause.code === Code.FailedPrecondition &&
        [
          "fleet node has no active control stream",
          "fleet node is not CONFIRMED",
          "fleet node is not confirmed; cannot pair until enrollment completes",
        ].includes(cause.rawMessage)))
  );
};

interface FindNodeMinersModalProps {
  node: FleetNodeItem;
  canPair: boolean;
  blockedPairingIdentifiers: string[];
  onDismiss: () => void;
  onUpdated: () => void;
  onPairingStarted: (identifiers: string[]) => void;
  onPairingCompleted: (identifiers: string[]) => void;
  onPairingSettledAfterDismiss: () => void;
}

const FindNodeMinersModal = ({
  node,
  canPair,
  blockedPairingIdentifiers,
  onDismiss,
  onUpdated,
  onPairingStarted,
  onPairingCompleted,
  onPairingSettledAfterDismiss,
}: FindNodeMinersModalProps) => {
  const { listFleetNodeDiscoveredDevices, discoverOnFleetNode, pairDiscoveredDevicesOnFleetNode } = useFleetNodes();
  const [discovered, setDiscovered] = useState<FleetNodeDiscoveredDevice[]>([]);
  const [nextCursor, setNextCursor] = useState(0n);
  const [selected, setSelected] = useState<string[]>([]);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [scanCount, setScanCount] = useState(0);
  const [pairResults, setPairResults] = useState<DevicePairingResult[]>([]);
  const [hasLoaded, setHasLoaded] = useState(false);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [isScanning, setIsScanning] = useState(false);
  const [isPairing, setIsPairing] = useState(false);
  const [error, setError] = useState("");
  const [warning, setWarning] = useState("");
  const discoveredGenerationRef = useRef(0);
  const mountedRef = useRef(true);
  const selectedIdentifiers = useMemo(() => new Set(selected), [selected]);
  const blockedIdentifiers = useMemo(() => new Set(blockedPairingIdentifiers), [blockedPairingIdentifiers]);

  const refresh = useCallback(async () => {
    const generation = ++discoveredGenerationRef.current;
    setIsLoadingMore(false);
    const discovery = await listFleetNodeDiscoveredDevices(node.fleetNodeId);
    if (generation !== discoveredGenerationRef.current) return;
    setDiscovered(discovery.devices);
    setNextCursor(discovery.nextCursor);
    setSelected([]);
    setHasLoaded(true);
  }, [listFleetNodeDiscoveredDevices, node.fleetNodeId]);

  const loadMore = async () => {
    if (nextCursor === 0n || isLoadingMore || isScanning || isPairing) return;
    const generation = discoveredGenerationRef.current;
    setIsLoadingMore(true);
    try {
      const next = await listFleetNodeDiscoveredDevices(node.fleetNodeId, nextCursor);
      if (generation !== discoveredGenerationRef.current) return;
      setDiscovered((current) => [...current, ...next.devices]);
      setNextCursor(next.nextCursor);
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
    setScanCount(0);
    setIsScanning(true);
    const seen = new Set<string>();
    try {
      await discoverOnFleetNode(
        node.fleetNodeId,
        create(DiscoverRequestSchema, { mode: { case: "networkScan", value: { useFleetNodeLocalSubnet: true } } }),
        (response) => {
          if (!mountedRef.current) return;
          if (response.warning) setWarning(response.warning);
          for (const device of response.devices) seen.add(device.deviceIdentifier);
          setScanCount(seen.size);
        },
      );
      if (mountedRef.current) await refresh();
    } catch (err) {
      if (mountedRef.current) setError(err instanceof Error ? err.message : "Discovery failed.");
    } finally {
      if (mountedRef.current) setIsScanning(false);
    }
  }, [discoverOnFleetNode, node.fleetNodeId, refresh]);

  useEffect(() => {
    let active = true;
    const generationRef = discoveredGenerationRef;
    mountedRef.current = true;
    void Promise.resolve().then(() => {
      if (active) void scan();
    });
    return () => {
      active = false;
      mountedRef.current = false;
      generationRef.current++;
    };
  }, [scan]);

  const pairSelected = async () => {
    if (selected.length === 0 || selected.some((identifier) => blockedIdentifiers.has(identifier))) return;
    if (Boolean(username.trim()) !== Boolean(password)) {
      setError("Enter both a username and password, or leave both blank.");
      return;
    }
    setError("");
    setPairResults([]);
    setIsPairing(true);
    const submitted = [...selected];
    onPairingStarted(submitted);
    const unreported = new Set(submitted);
    let pairingCompleted = false;
    let receivedResults = false;
    try {
      const credentials = username.trim()
        ? create(CredentialsSchema, { username: username.trim(), password })
        : undefined;
      await pairDiscoveredDevicesOnFleetNode(node.fleetNodeId, submitted, credentials, (results) => {
        receivedResults = true;
        const completed = results
          .map((result) => result.deviceIdentifier)
          .filter((identifier) => unreported.delete(identifier));
        if (completed.length > 0) onPairingCompleted(completed);
        if (mountedRef.current) {
          setPairResults((current) => [...current, ...results]);
          const paired = new Set(
            results
              .filter(
                (result) =>
                  result.pairingStatus === PairingStatus.PAIRED ||
                  result.pairingStatus === PairingStatus.DEFAULT_PASSWORD,
              )
              .map((result) => result.deviceIdentifier),
          );
          if (paired.size > 0)
            setDiscovered((current) => current.filter((device) => !paired.has(device.deviceIdentifier)));
        }
      });
      pairingCompleted = true;
      if (unreported.size > 0) onPairingCompleted([...unreported]);
      if (mountedRef.current) {
        setSelected([]);
        setPassword("");
        setHasLoaded(false);
      }
      if (mountedRef.current) await refresh();
      onUpdated();
    } catch (err) {
      // A disconnected result stream does not mean the Node stopped pairing.
      if (mountedRef.current) setSelected([]);
      if (!pairingCompleted && !receivedResults && isDefinitivePairRejection(err)) {
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

  const toggleSelected = (identifier: string) => {
    setSelected((current) =>
      current.includes(identifier)
        ? current.filter((value) => value !== identifier)
        : current.length < MAX_PAIRING_SELECTION
          ? [...current, identifier]
          : current,
    );
  };

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title={`Find miners on ${node.name}`}
      size="large"
      testId="find-node-miners-modal"
    >
      <div className="flex flex-col gap-6">
        {error ? (
          <div role="alert" className="text-intent-critical-fill">
            {error}
          </div>
        ) : null}
        <section className="flex flex-col items-start gap-3">
          <p className="text-300 text-text-primary-50">Scanning the network reachable from this Node.</p>
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Scan again"
            loading={isScanning}
            disabled={isPairing || !node.controlStreamConnected || node.commandProtocolUpgradeRequired}
            onClick={() => void scan()}
          />
          {isScanning || scanCount > 0 ? (
            <div role="status" className="text-300">
              {isScanning ? "Scanning" : "Scan complete"}: {scanCount} found
            </div>
          ) : null}
          {warning ? (
            <div role="status" className="text-intent-warning-fill">
              Discovery incomplete: {warning}
            </div>
          ) : null}
        </section>

        {hasLoaded ? (
          <section>
            <h3 className="mb-2 text-heading-200">Discovered miners</h3>
            {discovered.length === 0 ? <p className="text-text-primary-50">No unpaired miners discovered.</p> : null}
            <div className="max-h-72 overflow-y-auto">
              {discovered.map((device) => {
                const isSelected = selectedIdentifiers.has(device.deviceIdentifier);
                const isBlocked = blockedIdentifiers.has(device.deviceIdentifier);
                return (
                  <label
                    key={device.deviceIdentifier}
                    className="flex items-start gap-3 border-b border-border-10 py-2 text-300"
                  >
                    {canPair ? (
                      <input
                        type="checkbox"
                        checked={isSelected}
                        disabled={isBlocked || (!isSelected && selected.length >= MAX_PAIRING_SELECTION)}
                        onChange={() => toggleSelected(device.deviceIdentifier)}
                        aria-label={`Select ${device.deviceIdentifier}`}
                      />
                    ) : null}
                    <span className="min-w-0 break-all">
                      {device.deviceIdentifier} · {device.ipAddress}
                      {device.model ? ` · ${device.model}` : ""}
                      {device.pairingStatus === "AUTHENTICATION_NEEDED" ? " · Authentication needed" : ""}
                    </span>
                  </label>
                );
              })}
            </div>
            {nextCursor !== 0n ? (
              <Button
                variant={variants.textOnly}
                text={`Load next ${DISCOVERED_PAGE_SIZE}`}
                loading={isLoadingMore}
                disabled={isScanning || isPairing}
                onClick={() => void loadMore()}
              />
            ) : null}
          </section>
        ) : null}

        {hasLoaded && canPair && discovered.length > 0 ? (
          <section className="flex flex-col gap-3 border-t border-border-10 pt-4">
            <h3 className="text-heading-200">Add found miners</h3>
            <div className="text-300 text-text-primary-50">Leave credentials blank to use device defaults.</div>
            {selected.length === MAX_PAIRING_SELECTION ? (
              <div className="text-300 text-text-primary-50">
                Maximum of {MAX_PAIRING_SELECTION} miners per request.
              </div>
            ) : null}
            {blockedPairingIdentifiers.length > 0 ? (
              <div role="status" className="text-300 text-intent-warning-fill">
                {blockedPairingIdentifiers.length}{" "}
                {blockedPairingIdentifiers.length === 1 ? "miner has" : "miners have"} an unknown pairing result. Check
                their state before retrying; reload this page after checking to clear this block.
              </div>
            ) : null}
            <div className="grid grid-cols-2 gap-3 phone:grid-cols-1">
              <Input id="nodePairUsername" label="Username" initValue={username} onChange={setUsername} />
              <Input
                id="nodePairPassword"
                label="Password"
                type="password"
                initValue={password}
                onChange={setPassword}
              />
            </div>
            <Button
              variant={variants.primary}
              size={sizes.compact}
              text={`Add selected (${selected.length})`}
              disabled={
                selected.length === 0 ||
                isScanning ||
                !node.controlStreamConnected ||
                node.commandProtocolUpgradeRequired
              }
              loading={isPairing}
              onClick={() => void pairSelected()}
              className="self-start"
            />
          </section>
        ) : null}
        {pairResults.length > 0 ? (
          <section>
            <h3 className="mb-2 text-heading-200">Pairing results</h3>
            <ul className="text-300" aria-label="Pairing results">
              {pairResults.map((result) => (
                <li key={result.deviceIdentifier}>
                  {result.deviceIdentifier}: {PairingStatus[result.pairingStatus] ?? "Unknown"}
                  {result.error ? ` · ${result.error}` : ""}
                </li>
              ))}
            </ul>
          </section>
        ) : null}
      </div>
    </Modal>
  );
};

export default FindNodeMinersModal;
