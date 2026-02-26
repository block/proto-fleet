import MinerIssues from "./MinerIssues";

type MinerIssuesCellProps = {
  deviceIdentifier: string;
  onOpenStatusFlow: (deviceIdentifier: string) => void;
};

const MinerIssuesCell = ({ deviceIdentifier, onOpenStatusFlow }: MinerIssuesCellProps) => {
  return <MinerIssues deviceIdentifier={deviceIdentifier} onClick={() => onOpenStatusFlow(deviceIdentifier)} />;
};

export default MinerIssuesCell;
