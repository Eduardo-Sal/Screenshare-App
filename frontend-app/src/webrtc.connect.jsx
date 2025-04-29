export default async function connectToPi(deviceIP, onFrame, onStatus) {
    const pc = new RTCPeerConnection({
      iceServers: [
        { urls: 'stun:stun.l.google.com:19302' },
        { urls: `turn:${deviceIP}:3478`, username: 'user', credential: 'pass' }
      ]
    });
  
    const ws = new WebSocket(`ws://${deviceIP}:8000/ws`);
    const pendingCandidates = [];
  
    ws.onopen = async () => {
      onStatus('WebSocket connected');
  
      const dc = pc.createDataChannel('media');
      dc.binaryType = 'arraybuffer';
  
      dc.onopen = () => onStatus('Streaming...');
      dc.onmessage = (evt) => {
        const blob = new Blob([evt.data], { type: 'image/jpeg' });
        const url = URL.createObjectURL(blob);
        onFrame(url);
        setTimeout(() => URL.revokeObjectURL(url), 1000);
      };
  
      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
      ws.send(JSON.stringify({ type: 'offer', sdp: offer.sdp }));
    };
  
    pc.onicecandidate = (event) => {
      if (event.candidate && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'ice-candidate', candidate: event.candidate }));
      }
    };
  
    ws.onmessage = async (msg) => {
      const data = JSON.parse(msg.data);
  
      if (data.type === 'answer') {
        try {
          if (pc.signalingState === 'stable') {
            console.warn('Skipping setRemoteDescription: already stable');
            return;
          }
      
          await pc.setRemoteDescription(
            new RTCSessionDescription({
              type: 'answer',
              sdp: data.sdp
            })
          );
      
          // flush ICE candidates
          for (const candidate of pendingCandidates) {
            try {
              await pc.addIceCandidate(candidate);
            } catch (err) {
              console.warn('Failed to apply buffered ICE candidate:', err);
            }
          }
          pendingCandidates.length = 0;
        } catch (err) {
          console.error('Failed to set remote answer:', err);
        }
      }
  
      else if (data.type === 'ice-candidate') {
        const candidate = new RTCIceCandidate(data.candidate);
        if (pc.remoteDescription && pc.remoteDescription.type) {
          try {
            await pc.addIceCandidate(candidate);
          } catch (err) {
            console.warn('Failed to add ICE candidate:', err);
          }
        } else {
          // buffer ICE until remote desc is set
          pendingCandidates.push(candidate);
        }
      }
    };
  
    ws.onerror = () => onStatus('WebSocket error');
    ws.onclose = () => onStatus('Disconnected');
  
    pc.ondatachannel = (event) => {
      const dc = event.channel;
      dc.binaryType = 'arraybuffer';
      dc.onopen = () => onStatus('Streaming...');
      dc.onmessage = (evt) => {
        const blob = new Blob([evt.data], { type: 'image/jpeg' });
        const url = URL.createObjectURL(blob);
        onFrame(url);
        setTimeout(() => URL.revokeObjectURL(url), 1000);
      };
    };
  
    return () => {
      pc.close();
      ws.close();
    };
  }