import { useCallback, useState } from "react";

import { useCreatePool } from "api";

import SuccessIcon from "assets/icons/Success";

import Button, { sizes, variants } from "components/Button";
import Input from "components/Input";

const info = {
  url: "url",
  username: "username",
  password: "password",
};

const Onboarding = () => {
  // TODO: BTCM-1141 - add sidebar and onboarding steps
  const [poolInfo, setPoolInfo] = useState({
    [info.url]: "",
    [info.username]: "",
    [info.password]: "",
  });

  const [submitted, setSubmitted] = useState(false);

  const setInfo = useCallback(
    (value: string, info: string) => {
      setPoolInfo({ ...poolInfo, [info]: value });
      setSubmitted(false);
    },
    [poolInfo]
  );

  const { createPool, pending, error } = useCreatePool();

  const testConnection = useCallback(() => {
    createPool([poolInfo]).then(() => setSubmitted(true));
  }, [createPool, poolInfo]);

  return (
    <div className="mx-8 my-6">
      <div className="text-heading-200 mb-2">Add a mining pool</div>
      {/* TODO: add link to FAQ */}
      <div className="text-400 text-text-primary/50 mb-6">
        Input your desired mining pool URL and credentials. To learn more about
        mining pools or how to get started with a mining pool, visit our FAQ.
      </div>
      <Input
        id={info.url}
        label="Pool URL"
        maxLength={2083}
        onChange={setInfo}
      />
      <Input id={info.username} label="Username" onChange={setInfo} />
      <Input
        id={info.password}
        label="Password"
        type="password"
        onChange={setInfo}
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
            pending
          }
          loading={pending}
          prefixIcon={submitted && !error ? <SuccessIcon /> : undefined}
          text="Test Connection"
          size={sizes.compact}
          variant={variants.accent}
        />
      </div>
    </div>
  );
};

export default Onboarding;
