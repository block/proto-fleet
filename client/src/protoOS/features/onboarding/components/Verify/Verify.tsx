import MinerImage from "@/shared/assets/images/miner.png";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Picture from "@/shared/components/Picture";
import Row from "@/shared/components/Row";

type VerifyProps = {
  miner: {
    macAddress: string;
    serialNumber: string;
  };
  className?: string;
  handleContinueSetup: () => void;
};

const Verify = ({ miner, className, handleContinueSetup }: VerifyProps) => {
  return (
    <div className={className}>
      <div className="mx-auto flex flex-col gap-6">
        <div>
          <Header
            inline
            title="Is this the miner you want to set up?"
            titleSize="text-heading-300"
            description="If this matches the hashboard serial found on your packaging, continue to set up."
          />
        </div>
        <div className="rounded-2xl bg-surface-10 px-5 pt-10 pb-7">
          <div className="mx-auto sm:w-[600px]">
            <div className="mx-auto w-fit">
              <Picture className="mb-2 max-w-[228px]" image={MinerImage} />
              <div className="text-center text-heading-100 text-text-primary-50">Proto Rack</div>
            </div>
          </div>
        </div>
        <div>
          <Row className="grid grid-cols-2 gap-2">
            <div>Controller Serial</div>
            <div>Mac Address</div>
          </Row>
          <div className="max-h-[500px] overflow-y-auto">
            <Row className="grid grid-cols-2 gap-2">
              <div className="h-6">{miner.serialNumber}</div>
              <div className="h-6">{miner.macAddress}</div>
            </Row>
          </div>
        </div>
        <div className="flex justify-end gap-3">
          <Button onClick={handleContinueSetup} variant="primary" size="base">
            Continue setup
          </Button>
        </div>
      </div>
    </div>
  );
};

export default Verify;
