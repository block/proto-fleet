import { PoolInfo } from "../types";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Row from "@/shared/components/Row";

interface WarnDeleteDialogProps {
  keepBackup: () => void;
  onDelete: () => void;
  poolInfo: PoolInfo;
  show: boolean;
}

const InfoRow = ({ label, value }: { label: string; value: string }) => (
  <>
    <div className="text-emphasis-300 text-text-primary">{label}</div>
    <div className="text-200 text-text-primary-70">{value}</div>
  </>
);

const WarnDeleteDialog = ({
  keepBackup,
  onDelete,
  poolInfo,
  show,
}: WarnDeleteDialogProps) => {
  const showPoolUrl = poolInfo.url?.length > 0;
  const showUsername = poolInfo.username?.length > 0;
  return (
    <Dialog
      title="Delete backup pool?"
      preventScroll
      titleSize="text-heading-200"
      show={show}
      testId="warn-delete-dialog"
      buttons={[
        {
          text: "Delete backup",
          onClick: onDelete,
          variant: variants.danger,
          testId: "delete-backup-button",
        },
        {
          text: "Keep backup",
          onClick: keepBackup,
          variant: variants.primary,
          testId: "keep-backup-button",
        },
      ]}
    >
      {(showPoolUrl || showUsername) && (
        <div className="mt-4 rounded-lg border border-border-5 px-4 py-1">
          {showPoolUrl && (
            <Row divider={showUsername} compact>
              <InfoRow label="Pool URL" value={poolInfo.url} />
            </Row>
          )}
          {showUsername && (
            <Row divider={false} compact>
              <InfoRow label="Username" value={poolInfo.username} />
            </Row>
          )}
        </div>
      )}
    </Dialog>
  );
};

export default WarnDeleteDialog;
