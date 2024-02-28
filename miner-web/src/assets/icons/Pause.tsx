interface PauseProps {
  className?: string;
}

const Pause = ({ className }: PauseProps) => {
  return (
    <svg width="16" height="16" fill="none" xmlns="http://www.w3.org/2000/svg" className={className}>
      <g opacity=".8" fill="currentColor">
        <path d="M14 1h-4v14h4V1ZM7 1H3v14h4V1Z" />
      </g>
    </svg>
  );
};

export default Pause;
