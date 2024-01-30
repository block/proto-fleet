interface BlockProps {
  className: string;
  footer: string;
  hex: string;
}

const Block = ({ className, footer, hex }: BlockProps) => {
  return (
    <div className={`${className} h-16 w-80 p-4 flex flex-col`}>
      <div className="flex">
        <div className="grow">{footer}</div>
        {hex}
      </div>
    </div>
  );
};

export default {
  component: Block,
  title: "Colors",
};

export const Colors = () => {
  return (
    <div className="flex flex-col space-y-6">
      <div>
        Since we use Tailwind, by default, these colors will be made available
        everywhere in the framework where we use colors, like text color
        utilities, border color utilities, and background color utilities. That
        means you can prefix these class names with `text-`, `border-`, `bg-`,
        etc as needed. You can change them to different opacities by adding `/4`
        etc at the end.
      </div>
      <div className="flex space-x-4">
        <div className="flex flex-col space-y-6">
          <div className="flex flex-col border border-solid border-bg-dark h-fit">
            <Block
              className="bg-white-100 text-foreground-100"
              footer="white-100"
              hex="#FFFFFF"
            />
          </div>
          <div className="flex flex-col border border-solid border-bg-dark h-fit">
            <Block
              className="bg-tinted-10 text-black-100"
              footer="tinted-10"
              hex="#FFFDF5"
            />
            <Block
              className="bg-tinted-20 text-black-80"
              footer="tinted-20"
              hex="#F8F4E4"
            />
          </div>
          <div className="flex flex-col border border-solid border-bg-dark h-fit">
            <Block
              className="bg-primary-10 text-black-80"
              footer="primary-10"
              hex="#EBFBFF"
            />
            <Block
              className="bg-primary-50 text-black-80"
              footer="primary-50"
              hex="#A1C2C9"
            />
            <Block
              className="bg-primary-100 text-white-100"
              footer="primary-100"
              hex="#008096"
            />
          </div>
        </div>
        <div className="flex flex-col space-y-6">
          <div className="flex flex-col border border-solid border-bg-dark h-fit">
            <Block
              className="bg-success-100 text-foreground-100"
              footer="success-100"
              hex="#5EB04A"
            />
          </div>
          <div className="flex flex-col border border-solid border-bg-dark h-fit">
            <Block
              className="bg-warning-100/5 text-foreground-100"
              footer="warning-100/5"
              hex="#F46E38 (5% opacity)"
            />
            <Block
              className="bg-warning-100/10 text-foreground-100"
              footer="warning-100/10"
              hex="#F46E38 (10% opacity)"
            />
            <Block
              className="bg-warning-100 text-foreground-100"
              footer="warning-100"
              hex="#F46E38"
            />
          </div>
          <div className="flex flex-col border border-solid border-bg-dark h-fit">
            <Block
              className="bg-error-100 text-foreground-20"
              footer="error-100"
              hex="#CA0000"
            />
          </div>
        </div>
        <div className="flex flex-col space-y-6">
          <div className="flex flex-col border border-solid border-bg-dark h-fit">
            <Block
              className="bg-foreground-10 text-black-100"
              footer="foreground-10"
              hex="#F6F6F6"
            />
            <Block
              className="bg-foreground-20 text-black-100"
              footer="foreground-20"
              hex="#F8F8F8"
            />
            <Block
              className="bg-foreground-30 text-black-100"
              footer="foreground-30"
              hex="#C6C6C6"
            />
            <Block
              className="bg-foreground-60 text-white-100"
              footer="foreground-60"
              hex="#666666"
            />
            <Block
              className="bg-foreground-80 text-white-100"
              footer="foreground-80"
              hex="#4B4B4B"
            />
            <Block
              className="bg-foreground-100 text-white-100"
              footer="foreground-100"
              hex="#111111"
            />
          </div>
          <div className="flex flex-col border border-solid border-bg-dark h-fit">
            <Block
              className="bg-black-100/4 text-foreground-100"
              footer="black-100/4"
              hex="#000000 (4% opacity)"
            />
            <Block
              className="bg-black-100/5 text-foreground-100"
              footer="black-100/5"
              hex="#000000 (5% opacity)"
            />
            <Block
              className="bg-black-80 text-white-100"
              footer="black-80"
              hex="#161616"
            />
            <Block
              className="bg-black-100 text-white-100"
              footer="black-100"
              hex="#000000"
            />
          </div>
        </div>
      </div>
    </div>
  );
};
