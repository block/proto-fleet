interface ErrorProps {
  onClickRetry: () => void;
  text: string;
}

const Error = ({ onClickRetry, text }: ErrorProps) => {
  return (
    <>
      We’re having trouble connecting to your {text}. You can continue to your
      dashboard and adjust {text} or{" "}
      <button className="underline" onClick={onClickRetry}>
        try connecting again
      </button>
      .
    </>
  );
};

export default Error;
