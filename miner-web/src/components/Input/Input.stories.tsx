import Input from ".";

export const Single = () => {
  return (
    <>
      <Input
        id="poolUrl"
        label="Pool URL"
        onKeyUp={(value) => console.log(value)}
        maxLength={2083}
      />
    </>
  );
};

export const Multiple = () => {
  return (
    <>
      <Input
        id="poolUrl"
        label="Pool URL"
        onKeyUp={(value) => console.log(value)}
        maxLength={2083}
      />
      <Input
        id="username"
        label="Username"
        onKeyUp={(value) => console.log(value)}
      />
      <Input
        id="password"
        label="Password"
        onKeyUp={(value) => console.log(value)}
        type="password"
      />
    </>
  );
};

export default {
  component: Input,
  title: "Input",
};
