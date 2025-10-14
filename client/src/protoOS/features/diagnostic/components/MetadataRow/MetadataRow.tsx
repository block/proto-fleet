import Row from "@/shared/components/Row";

interface MetadataRowProps {
  label: string;
  value: string;
}

function MetadataRow({ label, value }: MetadataRowProps) {
  return (
    <Row className="flex justify-between" attributes={{ role: "row" }}>
      <div className="text-emphasis-300 text-text-primary">{label}</div>
      <div className="text-300 text-text-primary">{value}</div>
    </Row>
  );
}

export default MetadataRow;
