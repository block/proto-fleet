import { useCallback, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { type DiscoverRequest, DiscoverRequestSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";

type Mode = "ipList" | "ipRange" | "nmap";

interface DiscoveryFormProps {
  pending: boolean;
  onDiscover: (request: DiscoverRequest) => void;
  onCancel: () => void;
}

const modeOptions = [
  { value: "ipList", label: "IP list" },
  { value: "ipRange", label: "IP range" },
  { value: "nmap", label: "Subnet (nmap)" },
];

const splitPorts = (raw: string): string[] =>
  raw
    .split(",")
    .map((p) => p.trim())
    .filter(Boolean);

const DiscoveryForm = ({ pending, onDiscover, onCancel }: DiscoveryFormProps) => {
  const [mode, setMode] = useState<Mode>("ipList");
  const [ipAddresses, setIpAddresses] = useState("");
  const [startIp, setStartIp] = useState("");
  const [endIp, setEndIp] = useState("");
  const [target, setTarget] = useState("");
  const [ports, setPorts] = useState("");

  const buildRequest = useCallback((): DiscoverRequest | null => {
    const portList = splitPorts(ports);
    if (mode === "ipList") {
      const addrs = ipAddresses
        .split(/[\s,]+/)
        .map((a) => a.trim())
        .filter(Boolean);
      if (addrs.length === 0) return null;
      return create(DiscoverRequestSchema, {
        mode: { case: "ipList", value: { ipAddresses: addrs, ports: portList } },
      });
    }
    if (mode === "ipRange") {
      if (!startIp.trim() || !endIp.trim()) return null;
      return create(DiscoverRequestSchema, {
        mode: { case: "ipRange", value: { startIp: startIp.trim(), endIp: endIp.trim(), ports: portList } },
      });
    }
    if (!target.trim()) return null;
    return create(DiscoverRequestSchema, {
      mode: { case: "nmap", value: { target: target.trim(), ports: portList } },
    });
  }, [mode, ipAddresses, startIp, endIp, target, ports]);

  const handleSubmit = useCallback(() => {
    const request = buildRequest();
    if (request) onDiscover(request);
  }, [buildRequest, onDiscover]);

  return (
    <div className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
      <div className="text-heading-100 text-text-primary">Discover miners</div>
      <div className="max-w-xs">
        <Select
          id="discovery-mode"
          label="Mode"
          options={modeOptions}
          value={mode}
          onChange={(v) => setMode(v as Mode)}
        />
      </div>

      {mode === "ipList" ? (
        <Input
          id="ip-addresses"
          label="IP addresses (comma or space separated)"
          initValue={ipAddresses}
          onChange={setIpAddresses}
        />
      ) : null}

      {mode === "ipRange" ? (
        <div className="flex gap-4">
          <Input id="start-ip" label="Start IP" initValue={startIp} onChange={setStartIp} />
          <Input id="end-ip" label="End IP" initValue={endIp} onChange={setEndIp} />
        </div>
      ) : null}

      {mode === "nmap" ? (
        <Input id="nmap-target" label="Target (IP or CIDR)" initValue={target} onChange={setTarget} />
      ) : null}

      <Input id="ports" label="Ports (comma separated, optional)" initValue={ports} onChange={setPorts} />

      <div className="flex gap-2">
        <Button
          variant={variants.primary}
          size={sizes.compact}
          text="Discover"
          onClick={handleSubmit}
          loading={pending}
          testId="run-discovery"
        />
        {pending ? <Button variant={variants.secondary} size={sizes.compact} text="Cancel" onClick={onCancel} /> : null}
      </div>
    </div>
  );
};

export default DiscoveryForm;
