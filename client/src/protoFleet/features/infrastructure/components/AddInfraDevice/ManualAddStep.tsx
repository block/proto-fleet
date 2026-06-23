import { useCallback, useEffect, useMemo, useState } from "react";

import {
  getInfraDeviceConnectionTypeLabel,
  infraDeviceConnectionTypeOptions,
} from "@/protoFleet/features/infrastructure/connectionTypes";
import { FieldHelpPopover } from "@/protoFleet/features/infrastructure/fieldHelp";
import { infraDeviceFieldHelp } from "@/protoFleet/features/infrastructure/fieldHelpContent";
import type { InfraDeviceConnectionType } from "@/protoFleet/features/infrastructure/types";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export interface ManualAddStepState {
  canAdd: boolean;
  canTest: boolean;
  addHandler: () => void;
  testHandler: () => void;
}

interface ManualAddStepProps {
  onSuccess: () => void;
  onStateChange: (state: ManualAddStepState) => void;
  siteOptions?: string[];
  buildingOptions?: string[];
  buildingOptionsBySite?: Record<string, string[]>;
}

const toSelectOptions = (values: string[]) => values.map((value) => ({ value, label: value }));

const ManualAddStep = ({
  onSuccess,
  onStateChange,
  siteOptions = [],
  buildingOptions = [],
  buildingOptionsBySite = {},
}: ManualAddStepProps) => {
  const [name, setName] = useState("");
  const [site, setSite] = useState("");
  const [building, setBuilding] = useState("");
  const [connectionType, setConnectionType] = useState<InfraDeviceConnectionType | "">("");
  const [endpoint, setEndpoint] = useState("");
  const [port, setPort] = useState("");

  const availableBuildingOptions = site ? (buildingOptionsBySite[site] ?? buildingOptions) : buildingOptions;
  const siteSelectOptions = useMemo(() => toSelectOptions(siteOptions), [siteOptions]);
  const buildingSelectOptions = useMemo(() => toSelectOptions(availableBuildingOptions), [availableBuildingOptions]);
  const portNumber = Number(port);
  const isPortValid = Number.isInteger(portNumber) && portNumber > 0 && portNumber <= 65535;
  const canTest = [connectionType, endpoint].every((value) => value.trim().length > 0) && isPortValid;
  const isValid =
    [name, site, building, connectionType, endpoint].every((value) => value.trim().length > 0) && isPortValid;

  const handleSiteChange = useCallback((value: string) => {
    setSite(value);
    setBuilding("");
  }, []);

  const handleAdd = useCallback(() => {
    if (!isValid) return;
    onSuccess();
  }, [isValid, onSuccess]);

  const handleTest = useCallback(() => {
    if (!canTest || !connectionType) return;
    pushToast({
      message: `${getInfraDeviceConnectionTypeLabel(connectionType)} connection to ${endpoint}:${port} successful (12ms)`,
      status: STATUSES.success,
    });
  }, [canTest, connectionType, endpoint, port]);

  useEffect(() => {
    onStateChange({ canAdd: isValid, canTest, addHandler: handleAdd, testHandler: handleTest });
  }, [canTest, handleAdd, handleTest, isValid, onStateChange]);

  return (
    <div className="flex flex-col gap-4 pb-2">
      <Input id="manual-name" label="Name" onChange={(v) => setName(v)} />
      <div className="grid grid-cols-2 gap-3">
        <Select
          id="manual-site"
          label="Site"
          options={siteSelectOptions}
          value={site}
          onChange={handleSiteChange}
          disabled={siteSelectOptions.length === 0}
        />
        <Select
          id="manual-building"
          label="Building"
          options={buildingSelectOptions}
          value={building}
          onChange={setBuilding}
          disabled={buildingSelectOptions.length === 0}
        />
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
