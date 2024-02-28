interface ErrorProps {
  onClickRetry: () => void;
  text: string;
}

const Error = ({ onClickRetry, text }: ErrorProps) => {
  return (
    <>
      We’re having trouble connecting to your {text}. You can continue to your
      dashboard and adjust {text} or{" "}
      <span className="underline hover:cursor-pointer" onClick={onClickRetry}>
        try connecting again
      </span>
      .
    </>
  );
};

export default Error;
