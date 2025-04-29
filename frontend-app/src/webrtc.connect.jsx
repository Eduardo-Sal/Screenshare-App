// webrtc.connect.js
export default async function connectToPi(deviceIP, onFrame, onStatus) {
    const pc = new RTCPeerConnection({
      iceServers: [
        { urls: 'stun:stun.l.google.com:19302' },
        { urls: `turn:${deviceIP}:3478`, username: 'user', credential: 'pass' }
      ]
    });
  
    const ws = new WebSocket(`ws://${deviceIP}:8000/ws`);
    ws.onopen = () => onStatus('WebSocket connected');
    ws.onerror = () => onStatus('WebSocket error');
    ws.onclose = () => onStatus('Disconnected');
  
    pc.onicecandidate = (event) => {
      if (event.candidate) {
        ws.send(JSON.stringify({ type: 'ice-candidate', candidate: event.candidate }));
      }
    };
  
    pc.ondatachannel = (event) => {
      const dc = event.channel;
      dc.binaryType = 'arraybuffer';
      dc.onopen = () => onStatus('Streaming...');
      dc.onmessage = (evt) => {
        const blob = new Blob([evt.data], { type: 'image/jpeg' });
        const url = URL.createObjectURL(blob);
        onFrame(url);
      };
    };
  
    ws.onmessage = async (msg) => {
      const data = JSON.parse(msg.data);
      if (data.type === 'answer') {
        await pc.setRemoteDescription({ type: 'answer', sdp: data.sdp });
      } else if (data.type === 'ice-candidate') {
        await pc.addIceCandidate(data.candidate);
      }
    };
  
    const dc = pc.createDataChannel('dummy'); // initiator
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    ws.send(JSON.stringify({ type: 'offer', sdp: offer.sdp }));
  
    return () => {
      pc.close();
      ws.close();
    };
  }