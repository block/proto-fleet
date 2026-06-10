import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import {
  type DevicePairingResult,
  type FleetNodeDeviceSummary,
  type FleetNodeDiscoveredDevice,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { type DiscoverRequest } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import DiscoveryForm from "@/protoFleet/features/fleetNodes/components/DiscoveryForm";
import { pairingStatusLabel, pairingStatusTone } from "@/protoFleet/features/fleetNodes/utils/fleetNodeStatus";
import { ArrowLeftCompact } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import { type ColConfig, type ColTitles } from "@/shared/components/List/types";
import StatusCircle from "@/shared/components/StatusCircle";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type DiscoveredColKey = "deviceIdentifier" | "ipAddress" | "model" | "driverName" | "result";
type PairedColKey = "deviceIdentifier" | "deviceType";

const discoveredColTitles: ColTitles<DiscoveredColKey> = {
  deviceIdentifier: "Device",
  ipAddress: "IP address",
  model: "Model",
  driverName: "Driver",
  result: "Pairing",
};

const pairedColTitles: ColTitles<PairedColKey> = {
  deviceIdentifier: "Device",
  deviceType: "Type",
};

const pairedColConfig: ColConfig<FleetNodeDeviceSummary, bigint, PairedColKey> = {
  deviceIdentifier: { width: "min-w-64" },
  deviceType: { width: "min-w-32" },
};

const FleetNodeDetailPage = () => {
  const navigate = useNavigate();
  const { fleetNodeId: fleetNodeIdParam } = useParams<{ fleetNodeId: string }>();
  const fleetNodeId = useMemo(() => {
    try {
      return BigInt(fleetNodeIdParam ?? "");
    } catch {
      return null;
    }
  }, [fleetNodeIdParam]);

  const {
    listFleetNodes,
    discoverPending,
    discoverOnFleetNode,
    listDiscoveredDevices,
    pairingPending,
    pairDiscoveredDevices,
    listFleetNodeDevices,
  } = useFleetNodes();

  const [nodeName, setNodeName] = useState("");
  const [discovered, setDiscovered] = useState<Map<string, FleetNodeDiscoveredDevice>>(new Map());
  const [pairResults, setPairResults] = useState<Map<string, DevicePairingResult>>(new Map());
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const discoverAbort = useRef<AbortController | null>(null);

  const mergeDiscovered = useCallback((rows: FleetNodeDiscoveredDevice[]) => {
    setDiscovered((prev) => {
      const next = new Map(prev);
      rows.forEach((row) => next.set(row.deviceIdentifier, row));
      return next;
    });
  }, []);

  const refreshDiscovered = useCallback(() => {
    if (fleetNodeId === null) return;
    listDiscoveredDevices({ fleetNodeId, onSuccess: (rows) => mergeDiscovered(rows) });
  }, [fleetNodeId, listDiscoveredDevices, mergeDiscovered]);

  const [pairedDevices, setPairedDevices] = useState<FleetNodeDeviceSummary[]>([]);
  const refreshPaired = useCallback(() => {
    if (fleetNodeId === null) return;
    listFleetNodeDevices({ fleetNodeId, onSuccess: setPairedDevices });
  }, [fleetNodeId, listFleetNodeDevices]);

  useEffect(() => {
    if (fleetNodeId === null) return;
    listFleetNodes({
      onSuccess: (nodes) => {
        const node = nodes.find((n) => n.fleetNodeId === fleetNodeId);
        if (node) setNodeName(node.name);
      },
    });
    refreshDiscovered();
    refreshPaired();
  }, [fleetNodeId, listFleetNodes, refreshDiscovered, refreshPaired]);

  const handleDiscover = useCallback(
    (request: DiscoverRequest) => {
      if (fleetNodeId === null) return;
      const controller = new AbortController();
      discoverAbort.current = controller;
      discoverOnFleetNode({
        fleetNodeId,
        request,
        abortController: controller,
        onStreamData: (rows) => mergeDiscovered(rows),
        onError: (message) => pushToast({ message, status: STATUSES.error }),
        onFinally: refreshDiscovered,
      });
    },
    [fleetNodeId, discoverOnFleetNode, mergeDiscovered, refreshDiscovered],
  );

  const handleCancelDiscover = useCallback(() => {
    discoverAbort.current?.abort();
  }, []);

  const handlePairResults = useCallback((results: DevicePairingResult[]) => {
    setPairResults((prev) => {
      const next = new Map(prev);
      results.forEach((r) => next.set(r.deviceIdentifier, r));
      return next;
    });
  }, []);

  const runPair = useCallback(
    (deviceIdentifiers: string[], pairAllUnpaired: boolean) => {
      if (fleetNodeId === null) return;
      pairDiscoveredDevices({
        fleetNodeId,
        deviceIdentifiers,
        pairAllUnpaired,
        username: username.trim() || undefined,
        password: password || undefined,
        onResult: handlePairResults,
        onError: (message) => pushToast({ message, status: STATUSES.error }),
        onFinally: () => {
          refreshDiscovered();
          refreshPaired();
          setSelectedIds([]);
        },
      });
    },
    [fleetNodeId, pairDiscoveredDevices, username, password, handlePairResults, refreshDiscovered, refreshPaired],
  );

  const discoveredList = useMemo(() => Array.from(discovered.values()), [discovered]);

  const discoveredColConfig: ColConfig<FleetNodeDiscoveredDevice, string, DiscoveredColKey> = {
    deviceIdentifier: { width: "min-w-48" },
    ipAddress: { width: "min-w-32" },
    model: { width: "min-w-32" },
    driverName: { width: "min-w-28" },
    result: {
      width: "min-w-64",
      allowWrap: true,
      component: (device) => {
        const result = pairResults.get(device.deviceIdentifier);
        if (result) {
          return (
            <span className="flex flex-col gap-0.5">
              <span className="flex items-center gap-2">
                <StatusCircle status={pairingStatusTone(result.pairingStatus)} />
                {pairingStatusLabel(result.pairingStatus)}
              </span>
              {result.error ? (
                <span className="text-200 text-intent-critical-fill" title={result.error}>
                  {result.error}
                </span>
              ) : null}
            </span>
          );
        }
        if (device.pairingStatus === "AUTHENTICATION_NEEDED") {
          return <span className="text-text-primary-50">Auth needed</span>;
        }
        if (device.pairingStatus === "PAIRED") {
          return <span className="text-text-primary-50">Paired</span>;
        }
        return <span className="text-text-primary-50">—</span>;
      },
    },
  };

  if (fleetNodeId === null) {
    return <div className="px-6 pt-10 text-300 text-text-primary-70 laptop:px-10">Invalid fleet node id.</div>;
  }

  return (
    <div className="flex h-full flex-col gap-6">
      <div className="sticky left-0 z-10 flex flex-col gap-2 bg-surface-base px-6 pt-10 laptop:px-10">
        <Button
          variant={variants.ghost}
          size={sizes.compact}
          text="Fleet nodes"
          prefixIcon={<ArrowLeftCompact />}
          onClick={() => navigate("/fleet-nodes")}
        />
        <h1 className="text-heading-300 text-text-primary">{nodeName || `Fleet node #${fleetNodeId.toString()}`}</h1>
      </div>

      <div className="flex flex-col gap-6 px-6 laptop:px-10">
        <DiscoveryForm pending={discoverPending} onDiscover={handleDiscover} onCancel={handleCancelDiscover} />

        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-end justify-between gap-4">
            <div className="text-heading-100 text-text-primary">Discovered devices ({discoveredList.length})</div>
            <div className="flex flex-wrap items-end gap-3">
              <div className="w-40">
                <Input id="pair-username" label="Username (optional)" initValue={username} onChange={setUsername} />
              </div>
              <div className="w-40">
                <Input
                  id="pair-password"
                  label="Password (optional)"
                  type="password"
                  initValue={password}
                  onChange={setPassword}
                />
              </div>
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                text={`Pair selected (${selectedIds.length})`}
                onClick={() => runPair(selectedIds, false)}
                loading={pairingPending}
                disabled={selectedIds.length === 0}
              />
              <Button
                variant={variants.primary}
                size={sizes.compact}
                text="Pair all unpaired"
                onClick={() => runPair([], true)}
                loading={pairingPending}
                disabled={discoveredList.length === 0}
                testId="pair-all-unpaired"
              />
            </div>
          </div>

          {discoveredList.length === 0 ? (
            <div className="rounded-xl border border-dashed border-border-5 p-10 text-center text-300 text-text-primary-70">
              No devices discovered yet. Run a discovery above.
            </div>
          ) : (
            <List<FleetNodeDiscoveredDevice, string, DiscoveredColKey>
              items={discoveredList}
              itemKey="deviceIdentifier"
              itemSelectable
              customSelectedItems={selectedIds}
              customSetSelectedItems={setSelectedIds}
              activeCols={["deviceIdentifier", "ipAddress", "model", "driverName", "result"]}
              colTitles={discoveredColTitles}
              colConfig={discoveredColConfig}
            />
          )}
        </div>

        <div className="flex flex-col gap-3 pb-10">
          <div className="text-heading-100 text-text-primary">Paired devices ({pairedDevices.length})</div>
          {pairedDevices.length === 0 ? (
            <div className="rounded-xl border border-dashed border-border-5 p-10 text-center text-300 text-text-primary-70">
              No devices paired to this node yet.
            </div>
          ) : (
            <List<FleetNodeDeviceSummary, bigint, PairedColKey>
              items={pairedDevices}
              itemKey="deviceId"
              activeCols={["deviceIdentifier", "deviceType"]}
              colTitles={pairedColTitles}
              colConfig={pairedColConfig}
            />
          )}
        </div>
      </div>
    </div>
  );
};

export default FleetNodeDetailPage;
