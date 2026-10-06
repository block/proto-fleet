import BuildVersionInfo from "@/shared/components/BuildVersionInfo";

/**
 * Footer component for the ProtoFleet application
 * Includes version information and potentially other footer content
 */
const Footer = () => {
  return (
    <footer className="mt-auto border-t border-border-5 px-4 py-3 text-center">
      <BuildVersionInfo compact />
    </footer>
  );
};

export default Footer;
