import Row from "@/shared/components/Row";

interface AsicPopoverRowProps {
  label: string;
  value?: string | number;
}

const AsicPopoverRow = ({ label, value }: AsicPopoverRowProps) => {
  return (
    <Row className="flex items-center" divider={false} compact>
      <div className="grow text-emphasis-300 text-text-primary">{label}</div>
      <div className="text-300 text-text-primary">{value ?? "—"}</div>
    </Row>
  );
};

export default AsicPopoverRow;
