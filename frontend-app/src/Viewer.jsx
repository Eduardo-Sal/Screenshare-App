import { useEffect, useRef, useState } from 'react';
import connectToPi from './webrtc.connect';

export default function Viewer({ onClose, deviceIP }) {
  const [imgSrc, setImgSrc] = useState(null);
  const [status, setStatus] = useState('Connecting...');
  const cleanupRef = useRef(null);

  useEffect(() => {
    connectToPi(deviceIP, (blobUrl) => {
      setImgSrc(blobUrl);
      setTimeout(() => URL.revokeObjectURL(blobUrl), 1000);
    }, (updateStatus) => {
      setStatus(updateStatus);
    }).then((cleanupFn) => {
      cleanupRef.current = cleanupFn;
    });

    return () => {
      if (cleanupRef.current) cleanupRef.current();
    };
  }, [deviceIP]);

  return (
    <div style={{ background: '#121212', height: '100vh', color: '#f5f0e6', position: 'relative' }}>
      <button
        onClick={onClose}
        style={{
          position: 'absolute',
          top: '20px',
          right: '20px',
          background: '#f5f0e6',
          color: '#121212',
          border: 'none',
          borderRadius: '8px',
          padding: '8px 12px',
          fontWeight: 'bold',
          cursor: 'pointer'
        }}
      >
        ❌ Exit
      </button>

      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%' }}>
        {imgSrc ? (
          <img src={imgSrc} alt="Live Stream" style={{ maxWidth: '90%', maxHeight: '90%' }} />
        ) : (
          <p>{status}</p>
        )}
      </div>
    </div>
  );
}