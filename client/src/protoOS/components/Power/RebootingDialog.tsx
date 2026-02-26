import Dialog from "@/shared/components/Dialog";

interface RebootingDialogProps {
  open?: boolean;
}

const RebootingDialog = ({ open }: RebootingDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Rebooting miner"
      preventScroll
      subtitle="Your miner is rebooting. This may take a few minutes."
      subtitleSize="text-300"
      loading
      testId="rebooting-dialog"
    />
  );
};

export default RebootingDialog;
