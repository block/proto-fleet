import { useCallback, useMemo } from "react";

import type { InfraBuildingOption } from "@/protoFleet/features/infrastructure/types";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";

const buildOptions = (values: string[], currentValue: string) =>
  [...new Set([currentValue, ...values].filter(Boolean))].sort().map((value) => ({ value, label: value }));

interface CustomLocationInputProps {
  id: string;
  label: string;
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}

const CustomLocationInput = ({ id, label, value, disabled, onChange }: CustomLocationInputProps) => (
  <Input id={id} label={label} initValue={value} disabled={disabled} onChange={onChange} />
);

interface InfraLocationFieldsProps {
  site: string;
  building: string;
  siteOptions: string[];
  buildingOptions: InfraBuildingOption[];
  onSiteChange: (site: string) => void;
  onBuildingChange: (building: string) => void;
  allowCustomValues?: boolean;
  disabled?: boolean;
}

const InfraLocationFields = ({
  site,
  building,
  siteOptions,
  buildingOptions,
  onSiteChange,
  onBuildingChange,
  allowCustomValues = false,
  disabled = false,
}: InfraLocationFieldsProps) => {
  const siteSelectOptions = useMemo(() => buildOptions(siteOptions, site), [siteOptions, site]);
  const matchingBuildingNames = useMemo(
    () => buildingOptions.filter((option) => option.siteName === site).map((option) => option.buildingName),
    [buildingOptions, site],
  );
  const buildingSelectOptions = useMemo(
    () => buildOptions(matchingBuildingNames, building),
    [building, matchingBuildingNames],
  );

  const handleSiteChange = useCallback(
    (nextSite: string) => {
      onSiteChange(nextSite);

      const currentBuildingIsValid = buildingOptions.some(
        (option) => option.siteName === nextSite && option.buildingName === building,
      );
      if (currentBuildingIsValid) return;

      onBuildingChange(buildingOptions.find((option) => option.siteName === nextSite)?.buildingName ?? "");
    },
    [building, buildingOptions, onBuildingChange, onSiteChange],
  );

  const useCustomSiteInput = allowCustomValues && siteOptions.length === 0;
  const useCustomBuildingInput = allowCustomValues && (useCustomSiteInput || matchingBuildingNames.length === 0);

  return (
    <div className="grid grid-cols-2 gap-3">
      {useCustomSiteInput ? (
        <CustomLocationInput
          id="infra-location-site"
          label="Site"
          value={site}
          disabled={disabled}
          onChange={onSiteChange}
        />
      ) : (
        <Select
          id="infra-location-site"
          label="Site"
          options={siteSelectOptions}
          value={site}
          onChange={handleSiteChange}
          disabled={disabled || siteSelectOptions.length === 0}
          forceBelow
        />
      )}
      {useCustomBuildingInput ? (
        <CustomLocationInput
          key={useCustomSiteInput ? "custom-site-building" : site}
          id="infra-location-building"
          label="Building"
          value={building}
          disabled={disabled}
          onChange={onBuildingChange}
        />
      ) : (
        <Select
          id="infra-location-building"
          label="Building"
          options={buildingSelectOptions}
          value={building}
          onChange={onBuildingChange}
          disabled={disabled || site === "" || buildingSelectOptions.length === 0}
          forceBelow
        />
      )}
    </div>
  );
};

export default InfraLocationFields;
