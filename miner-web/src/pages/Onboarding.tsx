import { useCallback, useState } from "react";

import SuccessIcon from "assets/icons/Success";

import Button, { sizes, variants } from "components/Button";
import Input from "components/Input";

const info = {
  url: "url",
  user: "user",
  password: "password",
};

const status = {
  error: "error",
  loading: "loading",
  success: "success",
} as const;

const Onboarding = () => {
  // TODO: BTCM-1141 - add sidebar and onboarding steps
  const [poolInfo, setPoolInfo] = useState({
    [info.url]: "",
    [info.user]: "",
    [info.password]: "",
  });

  const [connectionStatus, setConnectionStatus] =
    useState<keyof typeof status>();

  const setInfo = useCallback(
    (value: string, info: string) => {
      setPoolInfo({ ...poolInfo, [info]: value });
      setConnectionStatus(undefined);
    },
    [poolInfo]
  );

  // TODO: call API when implemented
  const testConnection = useCallback(() => {
    setConnectionStatus(status.loading);
    setTimeout(() => {
      setConnectionStatus(status.success);
    }, 2000);
  }, []);

  return (
    <div className="mx-8 my-6">
      <div className="text-heading-200 mb-2">Add a mining pool</div>
      {/* TODO: add link to FAQ */}
      <div className="text-400 text-black-100/50 mb-6">
        Input your desired mining pool URL and credentials. To learn more about
        mining pools or how to get started with a mining pool, visit our FAQ.
      </div>
      <Input
        id={info.url}
        label="Pool URL"
        maxLength={2083}
        onKeyUp={setInfo}
      />
      <Input id={info.user} label="Username" onKeyUp={setInfo} />
      <Input
        id={info.password}
        label="Password"
        type="password"
        onKeyUp={setInfo}
      />
      <div className="flex mt-8">
        <div className="grow">
          <Button
            onClick={() => console.log("back")}
            text="Back"
            size={sizes.compact}
            variant={variants.secondary}
          />
        </div>
        <Button
          onClick={testConnection}
          disabled={
            !poolInfo[info.url] ||
            !poolInfo[info.user] ||
            !poolInfo[info.password] ||
            !!connectionStatus
          }
          loading={connectionStatus === status.loading}
          prefixIcon={
            connectionStatus === status.success ? <SuccessIcon /> : undefined
          }
          text="Test Connection"
          size={sizes.compact}
          variant={variants.accent}
        />
      </div>
    </div>
  );
};

export default Onboarding;
