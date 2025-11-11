import { useState } from 'react';

export interface MinefieldButtonProps {
  minefieldUrl?: string;
}

const MinefieldButton = ({ minefieldUrl = 'http://localhost:7070' }: MinefieldButtonProps) => {
  const [isOpen, setIsOpen] = useState(false);

  // Extract base URL and calculate control port
  let controlUrl = '';
  try {
    const url = new URL(minefieldUrl);
    const port = parseInt(url.port || '7070');
    const controlPort = port + 1;
    controlUrl = `${url.protocol}//${url.hostname}:${controlPort}`;
  } catch {
    controlUrl = 'http://localhost:7071';
  }

  return (
    <>
      {/* Floating button */}
      <button
        onClick={() => setIsOpen(true)}
        style={{
          position: 'fixed',
          bottom: '24px',
          right: '24px',
          zIndex: 50,
          display: 'flex',
          height: '56px',
          width: '56px',
          alignItems: 'center',
          justifyContent: 'center',
          borderRadius: '50%',
          backgroundColor: '#f97316',
          color: 'white',
          boxShadow: '0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -4px rgba(0, 0, 0, 0.1)',
          transition: 'all 0.2s',
          border: 'none',
          cursor: 'pointer'
        }}
        onMouseEnter={(e) => {
          e.currentTarget.style.backgroundColor = '#ea580c';
          e.currentTarget.style.boxShadow = '0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 8px 10px -6px rgba(0, 0, 0, 0.1)';
        }}
        onMouseLeave={(e) => {
          e.currentTarget.style.backgroundColor = '#f97316';
          e.currentTarget.style.boxShadow = '0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -4px rgba(0, 0, 0, 0.1)';
        }}
        title="Open Minefield Error Injector"
      >
        <svg
          width="24"
          height="24"
          viewBox="0 0 24 24"
          fill="currentColor"
          xmlns="http://www.w3.org/2000/svg"
          style={{ width: '24px', height: '24px' }}
        >
          {/* Main body (ring/donut shape) */}
          <path d="M12 4 C7.58 4 4 7.58 4 12 C4 16.42 7.58 20 12 20 C16.42 20 20 16.42 20 12 C20 7.58 16.42 4 12 4 Z M12 8 C14.21 8 16 9.79 16 12 C16 14.21 14.21 16 12 16 C9.79 16 8 14.21 8 12 C8 9.79 9.79 8 12 8 Z" />

          {/* Top spike */}
          <rect x="11" y="0" width="2" height="5" />
          <circle cx="12" cy="1" r="1.5" />

          {/* Bottom spike */}
          <rect x="11" y="19" width="2" height="5" />
          <circle cx="12" cy="23" r="1.5" />

          {/* Left spike */}
          <rect x="0" y="11" width="5" height="2" />
          <circle cx="1" cy="12" r="1.5" />

          {/* Right spike */}
          <rect x="19" y="11" width="5" height="2" />
          <circle cx="23" cy="12" r="1.5" />

          {/* Diagonal spikes */}
          <rect x="5" y="5" width="2" height="4" transform="rotate(-45 6 6)" />
          <circle cx="5" cy="5" r="1.5" />

          <rect x="17" y="5" width="2" height="4" transform="rotate(45 18 6)" />
          <circle cx="19" cy="5" r="1.5" />

          <rect x="5" y="17" width="2" height="4" transform="rotate(45 6 18)" />
          <circle cx="5" cy="19" r="1.5" />

          <rect x="17" y="17" width="2" height="4" transform="rotate(-45 18 18)" />
          <circle cx="19" cy="19" r="1.5" />

          {/* Chain attachment ring */}
          <circle cx="20" cy="20" r="0.5" fill="none" stroke="currentColor" strokeWidth="1" />
          <path d="M19.5 19.5 L21 21" stroke="currentColor" strokeWidth="0.5" />
        </svg>
      </button>

      {/* Modal overlay and content */}
      {isOpen && (
        <div
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            zIndex: 9999,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            backgroundColor: 'rgba(0, 0, 0, 0.5)',
            backdropFilter: 'blur(4px)'
          }}
          onClick={(e) => {
            // Only close if clicking the overlay, not the modal content
            if (e.target === e.currentTarget) {
              setIsOpen(false);
            }
          }}
        >
          <div style={{
            position: 'relative',
            height: '90vh',
            width: '90vw',
            maxWidth: '1280px',
            borderRadius: '8px',
            backgroundColor: 'white',
            boxShadow: '0 25px 50px -12px rgba(0, 0, 0, 0.25)'
          }}>
            {/* Modal header */}
            <div style={{
              display: 'flex',
              height: '56px',
              alignItems: 'center',
              justifyContent: 'space-between',
              borderBottom: '1px solid #e5e7eb',
              padding: '0 24px'
            }}>
              <div style={{
                display: 'flex',
                alignItems: 'center',
                gap: '12px'
              }}>
                <svg
                  width="20"
                  height="20"
                  viewBox="0 0 24 24"
                  fill="currentColor"
                  xmlns="http://www.w3.org/2000/svg"
                  style={{
                    height: '20px',
                    width: '20px',
                    color: '#f97316'
                  }}
                >
                  {/* Main body (ring/donut shape) */}
                  <path d="M12 4 C7.58 4 4 7.58 4 12 C4 16.42 7.58 20 12 20 C16.42 20 20 16.42 20 12 C20 7.58 16.42 4 12 4 Z M12 8 C14.21 8 16 9.79 16 12 C16 14.21 14.21 16 12 16 C9.79 16 8 14.21 8 12 C8 9.79 9.79 8 12 8 Z" />

                  {/* Top spike */}
                  <rect x="11" y="0" width="2" height="5" />
                  <circle cx="12" cy="1" r="1.5" />

                  {/* Bottom spike */}
                  <rect x="11" y="19" width="2" height="5" />
                  <circle cx="12" cy="23" r="1.5" />

                  {/* Left spike */}
                  <rect x="0" y="11" width="5" height="2" />
                  <circle cx="1" cy="12" r="1.5" />

                  {/* Right spike */}
                  <rect x="19" y="11" width="5" height="2" />
                  <circle cx="23" cy="12" r="1.5" />

                  {/* Diagonal spikes */}
                  <rect x="5" y="5" width="2" height="4" transform="rotate(-45 6 6)" />
                  <circle cx="5" cy="5" r="1.5" />

                  <rect x="17" y="5" width="2" height="4" transform="rotate(45 18 6)" />
                  <circle cx="19" cy="5" r="1.5" />

                  <rect x="5" y="17" width="2" height="4" transform="rotate(45 6 18)" />
                  <circle cx="5" cy="19" r="1.5" />

                  <rect x="17" y="17" width="2" height="4" transform="rotate(-45 18 18)" />
                  <circle cx="19" cy="19" r="1.5" />

                  {/* Chain attachment ring */}
                  <circle cx="20" cy="20" r="0.5" fill="none" stroke="currentColor" strokeWidth="1" />
                  <path d="M19.5 19.5 L21 21" stroke="currentColor" strokeWidth="0.5" />
                </svg>
                <h2 style={{
                  fontSize: '18px',
                  fontWeight: 600,
                  color: '#111827',
                  margin: 0
                }}>
                  Minefield Error Injector
                </h2>
                <span style={{
                  borderRadius: '4px',
                  backgroundColor: '#fed7aa',
                  padding: '2px 8px',
                  fontSize: '12px',
                  fontWeight: 500,
                  color: '#c2410c'
                }}>
                  DEV MODE
                </span>
              </div>
              <button
                onClick={() => setIsOpen(false)}
                style={{
                  borderRadius: '8px',
                  padding: '8px',
                  color: '#6b7280',
                  transition: 'all 0.2s',
                  backgroundColor: 'transparent',
                  border: 'none',
                  cursor: 'pointer'
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.backgroundColor = '#f3f4f6';
                  e.currentTarget.style.color = '#374151';
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.backgroundColor = 'transparent';
                  e.currentTarget.style.color = '#6b7280';
                }}
                aria-label="Close"
              >
                <svg
                  width="20"
                  height="20"
                  viewBox="0 0 24 24"
                  fill="none"
                  xmlns="http://www.w3.org/2000/svg"
                >
                  <path
                    d="M19 6.41L17.59 5L12 10.59L6.41 5L5 6.41L10.59 12L5 17.59L6.41 19L12 13.41L17.59 19L19 17.59L13.41 12L19 6.41Z"
                    fill="currentColor"
                  />
                </svg>
              </button>
            </div>

            {/* Iframe container */}
            <div style={{
              height: 'calc(100% - 56px)',
              width: '100%'
            }}>
              <iframe
                src={controlUrl}
                style={{
                  height: '100%',
                  width: '100%',
                  borderBottomLeftRadius: '8px',
                  borderBottomRightRadius: '8px',
                  border: 'none'
                }}
                title="Minefield Control Panel"
                sandbox="allow-same-origin allow-scripts allow-forms"
              />
            </div>
          </div>
        </div>
      )}
    </>
  );
};

export default MinefieldButton;
export { MinefieldButton };