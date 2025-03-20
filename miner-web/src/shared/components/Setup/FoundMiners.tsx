import { Logo } from "@/shared/assets/icons";
import MinerImage from "@/shared/assets/images/miner.png";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Row from "@/shared/components/Row";

type FoundMinersProps = {
  miners: {
    macAddress: string;
    controllerSerial: string;
  }[];
};

const FoundMiners = ({ miners }: FoundMinersProps) => {
  // TODO: Get real data from API

  return (
    <>
      <div>
        <Logo />
      </div>
      <div className="container mx-auto pt-20">
        <div className="mx-auto flex w-fit flex-col gap-6">
          <div>
            <Header
              inline
              title={
                miners.length === 1
                  ? "Is this the miner you want to set up?"
                  : `We found ${miners.length} Proto miners on your network`
              }
              titleSize="text-heading-300"
              description={
                miners.length === 1
                  ? "If this matches the hashboard serial found on your packaging, continue to set up."
                  : "Review the serial numbers below. If these match the materials on your packaging, continue to setup."
              }
            />
          </div>
          {miners.length === 1 && (
            <div className="rounded-2xl bg-surface-10 px-5 pt-10 pb-7">
              <div className="mx-auto sm:w-[600px]">
                <div className="mx-auto w-fit">
                  <img className="mb-2 max-w-[228px]" src={MinerImage} />
                  <div className="text-center text-heading-100 text-text-primary-50">
                    Proto Rack
                  </div>
                </div>
              </div>
            </div>
          )}
          <div>
            <div className="flex w-full justify-around">
              <div className="w-full">
                <Row>
                  <div>Controller Serial</div>
                </Row>
              </div>
              <div className="w-full">
                <Row>
                  <div>Mac Address</div>
                </Row>
              </div>
            </div>
            <div className="max-h-[500px] overflow-y-auto">
              {miners.map((miner, index) => (
                <div key={index} className="flex w-full justify-around">
                  <div className="w-full">
                    <Row>
                      <div>{miner.controllerSerial}</div>
                    </Row>
                  </div>
                  <div className="w-full">
                    <Row>
                      <div>{miner.macAddress}</div>
                    </Row>
                  </div>
                </div>
              ))}
            </div>
          </div>
          <div className="flex justify-end gap-3">
            {miners.length > 1 && (
              /* TODO: Restart search */
              <Button variant="secondary" size="base">
                Restart miner search
              </Button>
            )}
            {/* TODO: Add navigation to next step */}
            <Button variant="primary" size="base">
              Continue setup
            </Button>
          </div>
        </div>
      </div>
    </>
  );
};

export default FoundMiners;
