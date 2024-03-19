import { Link } from "react-router-dom";

const Settings = () => {
  return (
    <>
      <div className="mb-2">Settings page</div>
      {/* TODO: remove this when launching, this is only needed for staging */}
      <Link to="/onboarding">Onboarding</Link>
    </>
  )
};

export default Settings;
