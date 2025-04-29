// webrtc.connect.js
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
  
      // 1) Create your DataChannel (browser is the offerer)
      const dc = pc.createDataChannel('media');
      dc.binaryType = 'arraybuffer';
      dc.onopen = () => onStatus('Streaming…');
      dc.onmessage = (evt) => {
        console.log('Frame received', evt.data.byteLength);  // 👈 confirm receiving frames
        const blob = new Blob([evt.data], { type: 'image/png' });
        const url = URL.createObjectURL(blob);
        onFrame(url);
        setTimeout(() => URL.revokeObjectURL(URL.createObjectURL(blob)), 1000);
      };
  
      // 2) Make the offer
      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
  
      // 3) Wait for ICE gathering to finish so SDP has ice-ufrag/pwd
      await new Promise(resolve => {
        if (pc.iceGatheringState === 'complete') {
          resolve();
        } else {
          const check = () => {
            if (pc.iceGatheringState === 'complete') {
              pc.removeEventListener('icegatheringstatechange', check);
              resolve();
            }
          };
          pc.addEventListener('icegatheringstatechange', check);
        }
      });
  
      // 4) Send the fully-populated SDP
      if (!pc.localDescription) {
        console.error('Missing localDescription');
      } else {
        ws.send(JSON.stringify({
          type: 'offer',
          sdp: pc.localDescription.sdp
        }));
      }
    }
  
    ws.onerror = () => onStatus('WebSocket error');
    ws.onclose = () => onStatus('Disconnected');
  
    // Send ICE candidates as they come
    pc.onicecandidate = (evt) => {
      if (evt.candidate && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'ice-candidate', candidate: evt.candidate }));
      }
    };
  
    // Handle answer + remote ICE
    ws.onmessage = async (evt) => {
      const data = JSON.parse(evt.data);
      if (data.type === 'answer') {
        if (pc.signalingState !== 'stable') {
          await pc.setRemoteDescription(
            new RTCSessionDescription({ type: 'answer', sdp: data.sdp })
          );
          // flush queued candidates
          for (const c of pendingCandidates) {
            try { await pc.addIceCandidate(c); }
            catch (e) { console.warn('ICE add error', e); }
          }
          pendingCandidates.length = 0;
        }
      } else if (data.type === 'ice-candidate') {
        const c = new RTCIceCandidate(data.candidate);
        if (pc.remoteDescription && pc.remoteDescription.type) {
          try { await pc.addIceCandidate(c); }
          catch (e) { console.warn('ICE add error', e); }
        } else {
          pendingCandidates.push(c);
        }
      }
    };
  
    // Clean up
    return () => {
      pc.close();
      ws.close();
    };
  }