interface DismissProps {
  className?: string;
}

const Dismiss = ({ className }: DismissProps) => {
  return (
    <svg width="20" height="20" fill="none" xmlns="http://www.w3.org/2000/svg" className={className}>
      <path fill="#fff" fillOpacity=".01" d="M0 0h20v20H0z" />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="m3 1.586.707.707L10 8.586l6.293-6.293.707-.707L18.414 3l-.707.707L11.414 10l6.293 6.293.707.707L17 18.414l-.707-.707L10 11.414l-6.293 6.293-.707.707L1.586 17l.707-.707L8.586 10 2.293 3.707 1.586 3 3 1.586Z"
        fill="currentColor"
      />
    </svg>
  );
};

export default Dismiss;
