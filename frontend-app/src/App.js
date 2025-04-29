import { useState } from 'react';
import Viewer from './Viewer';
import './App.css';

function App() {
  const [streaming, setStreaming] = useState(false);
  const [deviceIP, setDeviceIP] = useState('');

  const handleStartStream = () => {
    if (!deviceIP) {
      alert('Please enter the Raspberry Pi IP address first!');
      return;
    }
    setStreaming(true);
  };

  return (
    <div className="App">
      <header className="App-header">
        {!streaming ? (
          <>
            {/* Home screen: Add Raspberry Pi and connect */}
           {/* <img src="./logo.svg" className="App-logo" alt="logo" />*/}
            <p>Welcome to Screenshare App</p>

            <input
              type="text"
              placeholder="Enter Raspberry Pi IP"
              value={deviceIP}
              onChange={(e) => setDeviceIP(e.target.value)}
              style={{
                padding: '8px',
                margin: '10px',
                borderRadius: '8px',
                border: '1px solid #ccc',
                width: '70%',
                textAlign: 'center'
              }}
            />

            <button onClick={handleStartStream}>
              Connect to Device
            </button>
          </>
        ) : (
          <>
            {/* Stream screen: Show Viewer component */}
            <Viewer onClose={() => setStreaming(false)} deviceIP={deviceIP} />
          </>
        )}
      </header>
    </div>
  );
}

export default App;