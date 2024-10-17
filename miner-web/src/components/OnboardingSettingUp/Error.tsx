interface ErrorProps {
  onClickRetry: () => void;
  text: string;
}

const Error = ({ onClickRetry, text }: ErrorProps) => {
  return (
    <div className="whitespace-normal">
      We’re having trouble connecting to your {text}. You can continue to your
      dashboard and adjust {text} or{" "}
      <button className="underline" onClick={onClickRetry}>
        try connecting again
      </button>
      .
    </div>
  );
};

export default Error;
