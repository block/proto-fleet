import type { ComponentMetadata as ComponentMetadataType } from "./types";

interface ComponentMetadataProps {
  metadata: ComponentMetadataType;
}

const MetadataRow = ({ label, value }: { label: string; value: string }) => (
  <div className="flex justify-between border-b border-border-5 py-2 last:border-0">
    <div className="text-emphasis-300 text-text-primary">{label}</div>
    <div className="text-300 text-text-primary">{value}</div>
  </div>
);

const ComponentMetadata = ({ metadata }: ComponentMetadataProps) => {
  return (
    <div className="flex flex-col">
      {metadata.serialNumber && (
        <MetadataRow label="Serial number" value={metadata.serialNumber} />
      )}
      {metadata.model && (
        <MetadataRow label="Model number" value={metadata.model} />
      )}
      {metadata.installedOn && (
        <MetadataRow label="Installed on" value={metadata.installedOn} />
      )}
      {metadata.age && <MetadataRow label="Age" value={metadata.age} />}
    </div>
  );
};

export default ComponentMetadata;
