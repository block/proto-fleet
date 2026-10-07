import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import NodeStatusBadge from "./NodeStatusBadge";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type {
  DevicePairingResult,
  FleetNodeDeviceSummary,
  FleetNodeDiscoveredDevice,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { CredentialsSchema, DiscoverRequestSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { getErrorCause } from "@/protoFleet/api/requestErrors";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import { getRelativeTimeFromEpoch } from "@/shared/utils/datetime";

const DISCOVERED_PAGE_SIZE = 100;
const PAIRED_PAGE_SIZE = 50;
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

interface NodeDetailsModalProps {
  node: FleetNodeItem;
  canManage: boolean;
  canPair: boolean;
  blockedPairingIdentifiers: string[];
  onDismiss: () => void;
  onUpdated: () => void;
  onPairingStarted: (identifiers: string[]) => void;
  onPairingCompleted: (identifiers: string[]) => void;
  onPairingSettledAfterDismiss: () => void;
}

const NodeDetailsModal = ({
  node,
  canManage,
  canPair,
  blockedPairingIdentifiers,
  onDismiss,
  onUpdated,
  onPairingStarted,
  onPairingCompleted,
  onPairingSettledAfterDismiss,
}: NodeDetailsModalProps) => {
  const {
    listFleetNodeDevices,
    listFleetNodeDiscoveredDevices,
    discoverOnFleetNode,
    pairDiscoveredDevicesOnFleetNode,
  } = useFleetNodes();
  const [paired, setPaired] = useState<FleetNodeDeviceSummary[]>([]);
  const [discovered, setDiscovered] = useState<FleetNodeDiscoveredDevice[]>([]);
  const [nextCursor, setNextCursor] = useState(0n);
  const [pairedLimit, setPairedLimit] = useState(PAIRED_PAGE_SIZE);
  const [selected, setSelected] = useState<string[]>([]);
  const [target, setTarget] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [scanCount, setScanCount] = useState(0);
  const [pairResults, setPairResults] = useState<DevicePairingResult[]>([]);
  const [isLoading, setIsLoading] = useState(true);
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

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const refresh = useCallback(async () => {
    const generation = ++discoveredGenerationRef.current;
    setIsLoadingMore(false);
    const [pairs, discovery] = await Promise.all([
      listFleetNodeDevices(node.fleetNodeId),
      listFleetNodeDiscoveredDevices(node.fleetNodeId),
    ]);
    if (generation !== discoveredGenerationRef.current) return;
    setPaired(pairs);
    setDiscovered(discovery.devices);
    setNextCursor(discovery.nextCursor);
    setSelected([]);
    setHasLoaded(true);
  }, [listFleetNodeDevices, listFleetNodeDiscoveredDevices, node.fleetNodeId]);

  useEffect(() => {
    let active = true;
    void Promise.all([listFleetNodeDevices(node.fleetNodeId), listFleetNodeDiscoveredDevices(node.fleetNodeId)])
      .then(([pairs, discovery]) => {
        if (!active) return;
        setPaired(pairs);
        setDiscovered(discovery.devices);
        setNextCursor(discovery.nextCursor);
        setHasLoaded(true);
      })
      .catch((err) => {
        if (active) setError(err instanceof Error ? err.message : "Failed to load Node details.");
      })
      .finally(() => {
        if (active) setIsLoading(false);
      });
    return () => {
      active = false;
    };
  }, [listFleetNodeDevices, listFleetNodeDiscoveredDevices, node.fleetNodeId]);

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

  const scan = async () => {
    const scanTarget = target.trim();
    if (!scanTarget) {
      setError("Enter an IP address, hostname, subnet, or IP range to scan.");
      return;
    }
    setError("");
    setWarning("");
    setScanCount(0);
    setIsScanning(true);
    const seen = new Set<string>();
    try {
      await discoverOnFleetNode(
        node.fleetNodeId,
        create(DiscoverRequestSchema, { mode: { case: "networkScan", value: { target: scanTarget } } }),
        (response) => {
          if (response.warning) setWarning(response.warning);
          for (const device of response.devices) seen.add(device.deviceIdentifier);
          setScanCount(seen.size);
        },
      );
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Discovery failed.");
    } finally {
      setIsScanning(false);
    }
  };

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
    let pairingCompleted = false;
    let receivedResults = false;
    try {
      const credentials = username.trim()
        ? create(CredentialsSchema, { username: username.trim(), password })
        : undefined;
      await pairDiscoveredDevicesOnFleetNode(node.fleetNodeId, submitted, credentials, (results) => {
        receivedResults = true;
        if (mountedRef.current) setPairResults((current) => [...current, ...results]);
      });
      pairingCompleted = true;
      onPairingCompleted(submitted);
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
        onPairingCompleted(submitted);
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
    <Modal open onDismiss={onDismiss} title={node.name} size="large" testId="node-details-modal">
      <div className="flex flex-col gap-6">
        <div className="grid grid-cols-2 gap-4 text-300 phone:grid-cols-1">
          <div>
            <div className="text-text-primary-50">Status</div>
            <NodeStatusBadge node={node} />
          </div>
          <div>
            <div className="text-text-primary-50">Last heartbeat</div>
            <div>{node.lastSeenAt ? getRelativeTimeFromEpoch(node.lastSeenAt.getTime()) : "Never"}</div>
          </div>
          <div>
            <div className="text-text-primary-50">Command connection</div>
            <div>{node.controlStreamConnected ? "Connected" : "Disconnected"}</div>
          </div>
          <div>
            <div className="text-text-primary-50">Identity fingerprint</div>
            <div className="font-mono">{node.identityFingerprint}</div>
          </div>
          <div>
            <div className="text-text-primary-50">Enrolled</div>
            <div>{node.createdAt?.toLocaleString() ?? "—"}</div>
          </div>
          <div>
            <div className="text-text-primary-50">Node ID</div>
            <div>{node.fleetNodeId}</div>
          </div>
        </div>

        {error ? (
          <div role="alert" className="text-intent-critical-fill">
            {error}
          </div>
        ) : null}
        {isLoading ? <div>Loading miners…</div> : null}

        {hasLoaded ? (
          <section>
            <h3 className="mb-2 text-heading-200">Paired miners ({paired.length})</h3>
            {paired.length === 0 ? <p className="text-text-primary-50">No paired miners.</p> : null}
            <ul className="max-h-64 overflow-y-auto">
              {paired.slice(0, pairedLimit).map((device) => (
                <li key={device.deviceId.toString()} className="border-b border-border-10 py-2 text-300">
                  {device.deviceIdentifier} {device.deviceType ? `· ${device.deviceType}` : ""}
                </li>
              ))}
            </ul>
            {pairedLimit < paired.length ? (
              <Button
                variant={variants.textOnly}
                text="Show more paired miners"
                onClick={() => setPairedLimit((limit) => limit + PAIRED_PAGE_SIZE)}
              />
            ) : null}
          </section>
        ) : null}

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
                    {canManage && canPair ? (
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

        {hasLoaded && canManage && node.controlStreamConnected && !node.commandProtocolUpgradeRequired ? (
          <section className="flex flex-col gap-3 border-t border-border-10 pt-4">
            <h3 className="text-heading-200">Discover on this Node</h3>
            <div className="text-300 text-text-primary-50">
              Scan an IP address, hostname, subnet, or IP range reachable from this Node.
            </div>
            <div className="flex items-end gap-3 phone:flex-col phone:items-stretch">
              <Input id="nodeScanTarget" label="Scan target" initValue={target} onChange={setTarget} />
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                text="Run discovery"
                loading={isScanning}
                disabled={isPairing}
                onClick={() => void scan()}
              />
            </div>
            {isScanning || scanCount > 0 ? (
              <div className="text-300">
                {isScanning ? "Scanning" : "Scan complete"}: {scanCount} found
              </div>
            ) : null}
            {warning ? (
              <div role="status" className="text-intent-warning-fill">
                Discovery incomplete: {warning}
              </div>
            ) : null}
          </section>
        ) : null}

        {hasLoaded && canManage && canPair && discovered.length > 0 ? (
          <section className="flex flex-col gap-3 border-t border-border-10 pt-4">
            <h3 className="text-heading-200">Pair selected miners</h3>
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
              text={`Pair selected (${selected.length})`}
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
            {pairResults.length > 0 ? (
              <ul className="text-300" aria-label="Pairing results">
                {pairResults.map((result) => (
                  <li key={result.deviceIdentifier}>
                    {result.deviceIdentifier}: {PairingStatus[result.pairingStatus] ?? "Unknown"}
                    {result.error ? ` · ${result.error}` : ""}
                  </li>
                ))}
              </ul>
            ) : null}
          </section>
        ) : null}
      </div>
    </Modal>
  );
};

export default NodeDetailsModal;
