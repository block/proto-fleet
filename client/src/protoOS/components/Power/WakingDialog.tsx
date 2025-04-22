import Dialog from "@/shared/components/Dialog";

interface WakingDialogProps {
  show: boolean;
}

const WakingDialog = ({ show }: WakingDialogProps) => {
  return (
    <Dialog
      title="Waking up miner"
      preventScroll
      subtitle="This may take a few seconds."
      subtitleSize="text-300"
      loading
      show={show}
      testId="waking-dialog"
    />
  );
};

export default WakingDialog;
