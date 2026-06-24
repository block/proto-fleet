import { useCallback, useEffect, useState } from "react";

import { infraDeviceConnectionTypeOptions } from "@/protoFleet/features/infrastructure/connectionTypes";
import { FieldHelpPopover } from "@/protoFleet/features/infrastructure/fieldHelp";
import { infraDeviceFieldHelp } from "@/protoFleet/features/infrastructure/fieldHelpContent";
import type { InfraDeviceConnectionType, InfraDeviceDraft } from "@/protoFleet/features/infrastructure/types";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";

export interface ManualAddStepState {
  canAdd: boolean;
  addHandler: () => void;
}

interface ManualAddStepProps {
  onSuccess: (device: InfraDeviceDraft) => void;
  onStateChange: (state: ManualAddStepState) => void;
}

const ManualAddStep = ({ onSuccess, onStateChange }: ManualAddStepProps) => {
  const [name, setName] = useState("");
  const [site, setSite] = useState("");
  const [building, setBuilding] = useState("");
  const [connectionType, setConnectionType] = useState<InfraDeviceConnectionType | "">("");
  const [endpoint, setEndpoint] = useState("");
  const [port, setPort] = useState("");

  const portNumber = Number(port);
  const isPortValid = Number.isInteger(portNumber) && portNumber > 0 && portNumber <= 65535;
  const isValid =
    [name, site, building, connectionType, endpoint].every((value) => value.trim().length > 0) && isPortValid;

  const handleSiteChange = useCallback((value: string) => {
    setSite(value);
  }, []);

  const handleAdd = useCallback(() => {
    if (!isValid || !connectionType) return;
    onSuccess({
      name: name.trim(),
      siteName: site.trim(),
      buildingName: building.trim(),
      connectionType,
      endpoint: endpoint.trim(),
      port: portNumber,
    });
  }, [building, connectionType, endpoint, isValid, name, onSuccess, portNumber, site]);

  useEffect(() => {
    onStateChange({ canAdd: isValid, addHandler: handleAdd });
  }, [handleAdd, isValid, onStateChange]);

  return (
    <div className="flex flex-col gap-4 pb-2">
      <Input id="manual-name" label="Name" onChange={(v) => setName(v)} />
      <div className="grid grid-cols-2 gap-3">
        <Input id="manual-site" label="Site" onChange={handleSiteChange} />
        <Input id="manual-building" label="Building" onChange={setBuilding} />
      </div>
      <Select
        id="manual-connection-type"
        label="Connection type"
        options={infraDeviceConnectionTypeOptions}
        value={connectionType}
        onChange={(value) => setConnectionType(value as InfraDeviceConnectionType)}
        suffixAction={<FieldHelpPopover {...infraDeviceFieldHelp.connectionType} />}
        forceBelow
      />
      <div className="grid grid-cols-2 gap-3">
        <Input
          id="manual-endpoint"
          label="Endpoint"
          suffixAction={<FieldHelpPopover {...infraDeviceFieldHelp.endpoint} />}
          onChange={(v) => setEndpoint(v)}
        />
        <Input
          id="manual-port"
          label="Port"
          type="number"
          inputMode="numeric"
          suffixAction={<FieldHelpPopover {...infraDeviceFieldHelp.port} />}
          onChange={(v) => setPort(v)}
        />
      </div>
    </div>
  );
};

export default ManualAddStep;
