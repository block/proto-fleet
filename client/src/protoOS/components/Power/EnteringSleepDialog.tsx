import Dialog from "@/shared/components/Dialog";

interface EnteringSleepDialogProps {
  open?: boolean;
}

const EnteringSleepDialog = ({ open }: EnteringSleepDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Entering sleep mode"
      preventScroll
      subtitle="Your miner is entering sleep mode. This may take a few seconds."
      subtitleSize="text-300"
      loading
      testId="entering-sleep-dialog"
    />
  );
};

export default EnteringSleepDialog;
