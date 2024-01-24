import ButtonComponent from ".";

export const Button = () => {
  return (
    <ButtonComponent
      text="Click me"
      className="w-64"
      onClick={() => {
        console.log("clicked");
      }}
    />
  );
};

export default {
  component: Button,
  title: "Button",
};
