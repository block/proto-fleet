import { useCallback, useState } from "react";

import { Api } from "Api";

import SuccessIcon from "assets/icons/Success";

import Button, { sizes, variants } from "components/Button";
import Input from "components/Input";

const info = {
  url: "url",
  username: "username",
  password: "password",
};

const status = {
  error: "error",
  loading: "loading",
  success: "success",
} as const;

const { api } = new Api();

const Onboarding = () => {
  // TODO: BTCM-1141 - add sidebar and onboarding steps
  const [poolInfo, setPoolInfo] = useState({
    [info.url]: "",
    [info.username]: "",
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

  const testConnection = useCallback(() => {
    setConnectionStatus(status.loading);
    api
      .createPool([poolInfo])
      .then(() => {
        setConnectionStatus(status.success);
      })
      .catch(() => {
        setConnectionStatus(status.error);
      });
  }, [poolInfo]);

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
      <Input id={info.username} label="Username" onKeyUp={setInfo} />
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
            !poolInfo[info.username] ||
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
