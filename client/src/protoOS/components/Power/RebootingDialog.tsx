import Dialog from "@/shared/components/Dialog";

interface RebootingDialogProps {
  show: boolean;
}

const RebootingDialog = ({ show }: RebootingDialogProps) => {
  return (
    <Dialog
      title="Rebooting miner"
      preventScroll
      subtitle="Your miner is rebooting. This may take a few minutes."
      subtitleSize="text-300"
      loading
      show={show}
      testId="rebooting-dialog"
    />
  );
};

export default RebootingDialog;
