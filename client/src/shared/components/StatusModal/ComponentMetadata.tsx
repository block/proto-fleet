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
      {Object.entries(metadata || {}).map(([_key, { label, value }]) =>
        value !== undefined ? (
          <MetadataRow
            label={label}
            value={String(value)}
            key={label + String(value)}
          />
        ) : null,
      )}
    </div>
  );
};

export default ComponentMetadata;
