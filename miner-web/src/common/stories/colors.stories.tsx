import { ReactNode } from "react";
import clsx from "clsx";

interface BlockProps {
  className: string;
  hex: string;
}

const Block = ({ className, hex }: BlockProps) => {
  return (
    <div className={`${className} h-16 p-4 w-full flex flex-col`}>
      <div className="flex">
        <div className="grow">{className.split(" ")[0].split("bg-")[1]}</div>
        {hex}
      </div>
    </div>
  );
};

interface ChildrenProps {
  children?: ReactNode;
  className?: string;
}

const Wrapper = ({ children, className }: ChildrenProps) => {
  return (
    <div className={clsx("flex flex-col space-y-6", className)}>
      <div className="flex space-x-4">{children}</div>
    </div>
  );
};

const Column = ({ children }: ChildrenProps) => {
  return <div className="flex flex-col space-y-6 w-1/3">{children}</div>;
};

const NestedCol = ({ children, className }: ChildrenProps) => {
  return (
    <div className={clsx("flex flex-col h-fit", className)}>{children}</div>
  );
};

export const Typography = () => {
  return (
    <Wrapper>
      <Column>
        <NestedCol>
          <Block className="bg-text-primary text-text-contrast" hex="#000" />
          <Block
            className="bg-text-primary/70 text-text-contrast"
            hex="70% opacity"
          />
          <Block
            className="bg-text-primary/50 text-text-contrast"
            hex="50% opacity"
          />
          <Block
            className="bg-text-primary/30 text-text-contrast"
            hex="30% opacity"
          />
        </NestedCol>
        <NestedCol className="border border-border-primary">
          <Block className="bg-text-contrast text-text-primary" hex="#FFF" />
          <Block
            className="bg-text-contrast/70 text-text-primary border-t border-border-primary"
            hex="70% opacity"
          />
        </NestedCol>
      </Column>
      <Column>
        <NestedCol>
          <Block
            className="bg-text-emphasis text-text-contrast"
            hex="#FF5B00"
          />
          <Block
            className="bg-text-accent text-text-contrast"
            hex="80% opacity"
          />
        </NestedCol>
      </Column>
      <Column>
        <NestedCol>
          <Block className="bg-text-success text-text-contrast" hex="#90C300" />
          <Block className="bg-text-warning text-text-contrast" hex="#FD8A00" />
          <Block
            className="bg-text-critical text-text-contrast"
            hex="#FA2B37"
          />
        </NestedCol>
      </Column>
    </Wrapper>
  );
};

export const Surface = () => {
  return (
    <Wrapper>
      <Column>
        <NestedCol className="border border-border-primary">
          <Block className="bg-surface-base text-text-primary" hex="#FFF" />
          <Block
            className="bg-surface-default text-text-primary border-t border-border-primary"
            hex="2% opacity"
          />
        </NestedCol>
      </Column>
      <Column>
        <NestedCol>
          <Block className="bg-surface-20 text-text-primary" hex="#CCC" />
          <Block className="bg-surface-10 text-text-primary" hex="#E5E5E5" />
          <Block className="bg-surface-5 text-text-primary" hex="#F2F2F2" />
        </NestedCol>
      </Column>
      <Column>
        <NestedCol>
          <Block
            className="bg-surface-overlay text-text-primary"
            hex="#000 (5% opacity)"
          />
        </NestedCol>
      </Column>
    </Wrapper>
  );
};

export const Border = () => {
  return (
    <Wrapper>
      <Column>
        <NestedCol>
          <Block className="bg-border-primary text-text-contrast" hex="#000" />
          <Block
            className="bg-border-primary/20 text-text-primary"
            hex="20% opacity"
          />
          <Block
            className="bg-border-primary/10 text-text-primary"
            hex="10% opacity"
          />
          <Block
            className="bg-border-primary/5 text-text-primary"
            hex="5% opacity"
          />
        </NestedCol>
      </Column>
    </Wrapper>
  );
};

export const Core = () => {
  return (
    <Wrapper>
      <Column>
        <NestedCol>
          <Block
            className="bg-core-primary-fill text-text-contrast"
            hex="#000"
          />
          <Block
            className="bg-core-primary/80 text-text-contrast"
            hex="80% opacity"
          />
          <Block
            className="bg-core-primary/50 text-text-contrast"
            hex="50% opacity"
          />
          <Block
            className="bg-core-primary/20 text-text-primary"
            hex="20% opacity"
          />
          <Block
            className="bg-core-primary/10 text-text-primary"
            hex="10% opacity"
          />
          <Block
            className="bg-core-primary/5 text-text-primary"
            hex="5% opacity"
          />
        </NestedCol>
      </Column>
      <Column>
        <NestedCol>
          <Block
            className="bg-core-accent-text text-text-contrast"
            hex="#331200"
          />
          <Block
            className="bg-core-accent-fill text-text-contrast"
            hex="#FF5B00"
          />
          <Block
            className="bg-core-accent-fill/80 text-text-contrast"
            hex="80% opacity"
          />
          <Block
            className="bg-core-accent-fill/50 text-text-primary"
            hex="50% opacity"
          />
          <Block
            className="bg-core-accent-fill/20 text-text-primary"
            hex="20% opacity"
          />
          <Block
            className="bg-core-accent-fill/10 text-text-primary"
            hex="10% opacity"
          />
        </NestedCol>
      </Column>
    </Wrapper>
  );
};

export const Intent = () => {
  return (
    <>
      <Wrapper>
        <Column>
          <NestedCol>
            <Block
              className="bg-intent-info-text text-text-contrast"
              hex="#004B73"
            />
            <Block
              className="bg-intent-info-fill text-text-contrast"
              hex="#00A4FB"
            />
            <Block
              className="bg-intent-info-fill/80 text-text-primary"
              hex="80% opacity"
            />
            <Block
              className="bg-intent-info-fill/50 text-text-primary"
              hex="50% opacity"
            />
            <Block
              className="bg-intent-info-fill/20 text-text-primary"
              hex="20% opacity"
            />
            <Block
              className="bg-intent-info-fill/10 text-text-primary"
              hex="10% opacity"
            />
          </NestedCol>
        </Column>
        <Column>
          <NestedCol>
            <Block
              className="bg-intent-success-text text-text-contrast"
              hex="#0B5C1F"
            />
            <Block
              className="bg-intent-success-fill text-text-contrast"
              hex="#00A4FB"
            />
            <Block
              className="bg-intent-success-fill/80 text-text-primary"
              hex="80% opacity"
            />
            <Block
              className="bg-intent-success-fill/50 text-text-primary"
              hex="50% opacity"
            />
            <Block
              className="bg-intent-success-fill/20 text-text-primary"
              hex="20% opacity"
            />
            <Block
              className="bg-intent-success-fill/10 text-text-primary"
              hex="10% opacity"
            />
          </NestedCol>
        </Column>
        <Column>
          <NestedCol>
            <Block
              className="bg-intent-warning-text text-text-contrast"
              hex="#874900"
            />
            <Block
              className="bg-intent-warning-fill text-text-contrast"
              hex="#00A4FB"
            />
            <Block
              className="bg-intent-warning-fill/80 text-text-primary"
              hex="80% opacity"
            />
            <Block
              className="bg-intent-warning-fill/50 text-text-primary"
              hex="50% opacity"
            />
            <Block
              className="bg-intent-warning-fill/20 text-text-primary"
              hex="20% opacity"
            />
            <Block
              className="bg-intent-warning-fill/10 text-text-primary"
              hex="10% opacity"
            />
          </NestedCol>
        </Column>
      </Wrapper>
      <Wrapper className="mt-4">
        <Column>
          <NestedCol>
            <Block
              className="bg-intent-critical-text text-text-contrast"
              hex="#74140D"
            />
            <Block
              className="bg-intent-critical-fill text-text-contrast"
              hex="#00A4FB"
            />
            <Block
              className="bg-intent-critical-fill/80 text-text-contrast"
              hex="80% opacity"
            />
            <Block
              className="bg-intent-critical-fill/50 text-text-primary"
              hex="50% opacity"
            />
            <Block
              className="bg-intent-critical-fill/20 text-text-primary"
              hex="20% opacity"
            />
            <Block
              className="bg-intent-critical-fill/10 text-text-primary"
              hex="10% opacity"
            />
          </NestedCol>
        </Column>
        <Column />
        <Column />
      </Wrapper>
    </>
  );
};

export const Grayscale = () => {
  return (
    <>
      <Wrapper>
        <Column>
          <NestedCol>
            <Block
              className="bg-grayscale-gray/50 text-text-primary"
              hex="#000 (50% opacity)"
            />
            <Block
              className="bg-grayscale-gray/20 text-text-primary"
              hex="20% opacity"
            />
            <Block
              className="bg-grayscale-gray/10 text-text-primary"
              hex="10% opacity"
            />
            <Block
              className="bg-grayscale-gray/5 text-text-primary"
              hex="5% opacity"
            />
          </NestedCol>
        </Column>
      </Wrapper>
    </>
  );
};

export default {
  title: "Colors",
};
