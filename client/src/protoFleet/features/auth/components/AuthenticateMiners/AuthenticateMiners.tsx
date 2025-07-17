import { sizes } from "@/shared/components/Button/constants";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";

type AuthenticateMinersProps = {
  onClose: () => void;
};

const AuthenticateMiners = ({ onClose }: AuthenticateMinersProps) => {
  return (
    <Modal
      divider={false}
      onDismiss={onClose}
      show
      buttons={[
        { variant: "textOnly", text: "Show miners" },
        { variant: "primary", text: "Authenticate" },
      ]}
      buttonSize={sizes.base}
    >
      <Header
        title="Authenticate miners"
        titleSize="text-heading-300"
        subtitle="If miners use different credentials, we'll try each attempt until all miners are configured."
        subtitleSize="text-300"
        className="mb-6"
      />
      <div className="rounded-2xl bg-surface-5 p-6">
        <div className="mb-2 flex gap-2">
          <div className="text-emphasis-300">Bulk authenticate</div>
          <div className="text-300">145 miners remaining</div>
        </div>
        <div className="flex w-full gap-4">
          <div className="w-full">
            <Input id="username" label="Username" />
          </div>
          <div className="w-full">
            <Input id="password" label="Password" type="password" />
          </div>
        </div>
      </div>
    </Modal>
  );
};

export default AuthenticateMiners;
