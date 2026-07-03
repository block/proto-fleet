import { useCallback, useMemo } from "react";

import type { InfraBuildingOption } from "@/protoFleet/features/infrastructure/types";
import Select from "@/shared/components/Select";

const buildOptions = (values: string[], currentValue: string) =>
  [...new Set([currentValue, ...values].filter(Boolean))].sort().map((value) => ({ value, label: value }));

interface InfraLocationFieldsProps {
  site: string;
  building: string;
  siteOptions: string[];
  buildingOptions: InfraBuildingOption[];
  onSiteChange: (site: string) => void;
  onBuildingChange: (building: string) => void;
  disabled?: boolean;
}

const InfraLocationFields = ({
  site,
  building,
  siteOptions,
  buildingOptions,
  onSiteChange,
  onBuildingChange,
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

  return (
    <div className="grid grid-cols-2 gap-3">
      <Select
        id="infra-location-site"
        label="Site"
        options={siteSelectOptions}
        value={site}
        onChange={handleSiteChange}
        disabled={disabled || siteSelectOptions.length === 0}
        forceBelow
      />
      <Select
        id="infra-location-building"
        label="Building"
        options={buildingSelectOptions}
        value={building}
        onChange={onBuildingChange}
        disabled={disabled || site === "" || buildingSelectOptions.length === 0}
        forceBelow
      />
    </div>
  );
};

export default InfraLocationFields;
