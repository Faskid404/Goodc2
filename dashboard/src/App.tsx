import { useCallback, useEffect, useRef, useState } from 'react';

const API = (import.meta.env.VITE_API_BASE ?? '').replace(/\/$/, '');
const WS_URL = (import.meta.env.VITE_WS_BASE ?? API).replace(/^http/, 'ws');

interface Agent {
  id: string; hostname: string; ip: string; os: string;
  arch: string; version: string; first_seen: string; last_seen: string; online: boolean;
}
interface Metrics {
  agent_id: string; cpu_pct: number; mem_total: number; mem_used: number;
  disk_total: number; disk_used: number; net_rx: number; net_tx: number;
  proc_count: number; recorded_at: string;
}
interface CmdRow { id: number; agent_id: string; type: string; payload: string; status: string; result: string; created_at: string; }
interface WireMsg { type: string; id?: number; payload?: unknown; }
interface AlertPayload { agent_id: string; hostname: string; ip: string; os: string; last_seen: string; kind: 'offline' | 'recovered'; message: string; }
interface Toast { id: number; kind: 'offline' | 'recovered' | 'up' | 'down'; hostname: string; message: string; }

const CMDS = [
  { type: 'get_metrics', label: 'Metrics',  icon: '📊', payload: '' },
  { type: 'get_disk',    label: 'Disk',     icon: '💾', payload: '' },
  { type: 'get_network', label: 'Network',  icon: '🌐', payload: '' },
  { type: 'get_procs',   label: 'Procs',    icon: '⚙️',  payload: '' },
  { type: 'get_sysinfo', label: 'SysInfo',  icon: '🖥️',  payload: '' },
  { type: 'ping',        label: 'Ping',     icon: '🏓', payload: '' },
];

let toastSeq = 0;

function fmtBytes(b: number) {
  if (!b) return '0 B';
  const k = 1024, u = ['B','KB','MB','GB','TB'];
  const i = Math.floor(Math.log(b) / Math.log(k));
  return `${(b / Math.pow(k, i)).toFixed(1)} ${u[i]}`;
}
function pct(used: number, total: number) { return total ? Math.round((used / total) * 100) : 0; }
function ago(iso: string) {
  const d = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (d < 60) return `${d}s ago`;
  if (d < 3600) return `${Math.floor(d / 60)}m ago`;
  return `${Math.floor(d / 3600)}h ago`;
}

/* ── sparkline ── */
function Spark({ data, color }: { data: number[]; color: string }) {
  const w = 72, h = 24;
  if (data.length < 2) return null;
  const max = Math.max(...data, 1);
  const pts = data.map((v, i) => `${(i / (data.length - 1)) * w},${h - 2 - (v / max) * (h - 4)}`).join(' ');
  return (
    <svg width={w} height={h} className="opacity-60">
      <polyline fill="none" stroke={color} strokeWidth="1.5" strokeLinejoin="round" points={pts} />
    </svg>
  );
}

/* ── bar ── */
function Bar({ p, color = '#06b6d4' }: { p: number; color?: string }) {
  return (
    <div className="h-1.5 w-full rounded-full overflow-hidden" style={{ background: 'rgba(255,255,255,0.07)' }}>
      <div className="h-full rounded-full transition-all duration-700" style={{ width: `${Math.min(p, 100)}%`, background: color }} />
    </div>
  );
}

/* ── toasts ── */
function Toasts({ toasts, remove }: { toasts: Toast[]; remove: (id: number) => void }) {
  if (!toasts.length) return null;
  return (
    <div className="fixed top-4 right-4 z-50 flex flex-col gap-2 w-80">
      {toasts.map(t => {
        const isOffline = t.kind === 'offline' || t.kind === 'down';
        const isRecovered = t.kind === 'recovered' || t.kind === 'up';
        const borderColor = isOffline ? '#ef4444' : isRecovered ? '#10b981' : '#06b6d4';
        const iconBg = isOffline ? 'rgba(239,68,68,0.15)' : isRecovered ? 'rgba(16,185,129,0.15)' : 'rgba(6,182,212,0.15)';
        const icon = isOffline ? '⚠' : isRecovered ? '✓' : '↑';
        return (
          <div key={t.id} className="flex items-start gap-3 p-3 rounded-xl shadow-2xl animate-slide-in"
            style={{ background: '#0f172a', border: `1px solid ${borderColor}`, boxShadow: `0 0 20px ${borderColor}22` }}>
            <div className="w-7 h-7 rounded-lg shrink-0 flex items-center justify-center text-sm font-bold"
              style={{ background: iconBg, color: borderColor }}>
              {icon}
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-xs font-semibold text-white truncate">{t.hostname}</p>
              <p className="text-xs mt-0.5" style={{ color: '#64748b' }}>{t.message}</p>
            </div>
            <button onClick={() => remove(t.id)} className="text-slate-600 hover:text-slate-400 text-xs shrink-0 mt-0.5">✕</button>
          </div>
        );
      })}
    </div>
  );
}

/* ── alert badge ── */
function AlertBadge({ count }: { count: number }) {
  if (!count) return null;
  return (
    <span className="inline-flex items-center justify-center w-4 h-4 rounded-full text-xs font-bold text-white"
      style={{ background: '#ef4444', fontSize: 9 }}>
      {count > 9 ? '9+' : count}
    </span>
  );
}

/* ── login ── */
function Login({ onToken }: { onToken: (t: string) => void }) {
  const [pw, setPw] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('');
    try {
      const r = await fetch(`${API}/api/auth/login`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password: pw }) });
      if (!r.ok) { setErr('Wrong password'); return; }
      const { token } = await r.json();
      localStorage.setItem('c2tok', token);
      onToken(token);
    } catch { setErr('Connection failed'); }
    finally { setBusy(false); }
  };
  return (
    <div className="min-h-screen flex items-center justify-center" style={{ background: 'radial-gradient(ellipse at 50% 0%, #0f172a 0%, #020617 70%)' }}>
      <div className="w-full max-w-sm px-4">
        <div className="mb-10 text-center">
          <div className="inline-flex items-center justify-center w-14 h-14 rounded-2xl mb-4" style={{ background: 'linear-gradient(135deg,#06b6d4,#7c3aed)' }}>
            <svg className="w-7 h-7 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 3H5a2 2 0 00-2 2v4m6-6h10a2 2 0 012 2v4M9 3v18m0 0h10a2 2 0 002-2V9M9 21H5a2 2 0 01-2-2V9m0 0h18" />
            </svg>
          </div>
          <h1 className="text-2xl font-bold text-white tracking-tight">C2 Monitor</h1>
          <p className="text-slate-500 text-sm mt-1">Infrastructure observation platform</p>
        </div>
        <form onSubmit={submit} className="space-y-3">
          <input type="password" value={pw} onChange={e => setPw(e.target.value)} placeholder="Dashboard password" autoFocus
            className="w-full rounded-xl px-4 py-3 text-white placeholder-slate-600 outline-none border text-sm transition-colors"
            style={{ background: '#0f172a', borderColor: err ? '#ef4444' : '#1e293b' }}
            onFocus={e => { if (!err) e.currentTarget.style.borderColor = '#06b6d4'; }}
            onBlur={e => { e.currentTarget.style.borderColor = err ? '#ef4444' : '#1e293b'; }} />
          {err && <p className="text-red-400 text-xs px-1">{err}</p>}
          <button type="submit" disabled={busy || !pw}
            className="w-full py-3 rounded-xl text-sm font-semibold text-white disabled:opacity-40 transition-opacity"
            style={{ background: 'linear-gradient(135deg,#06b6d4,#7c3aed)' }}>
            {busy ? 'Signing in…' : 'Sign in →'}
          </button>
        </form>
      </div>
    </div>
  );
}

/* ── main ── */
export default function App() {
  const [token, setToken] = useState(() => localStorage.getItem('c2tok') ?? '');
  const [agents, setAgents] = useState<Agent[]>([]);
  const [sel, setSel] = useState<string | null>(null);
  const [liveM, setLiveM] = useState<Record<string, Metrics>>({});
  const [cpuHist, setCpuHist] = useState<Record<string, number[]>>({});
  const [wsState, setWsState] = useState<'off' | 'on'>('off');
  const [cmds, setCmds] = useState<CmdRow[]>([]);
  const [tab, setTab] = useState<'metrics' | 'commands' | 'events'>('metrics');
  const [beaconSecs, setBeaconSecs] = useState('30');
  const [sending, setSending] = useState(false);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [offlineAgents, setOfflineAgents] = useState<Set<string>>(new Set());
  const wsRef = useRef<WebSocket | null>(null);

  const addToast = useCallback((t: Omit<Toast, 'id'>) => {
    const id = ++toastSeq;
    setToasts(p => [{ ...t, id }, ...p].slice(0, 5));
    setTimeout(() => setToasts(p => p.filter(x => x.id !== id)), 6000);
  }, []);

  const removeToast = useCallback((id: number) => setToasts(p => p.filter(x => x.id !== id)), []);

  const loadAgents = useCallback(async () => {
    if (!token) return;
    try {
      const r = await fetch(`${API}/api/agents`, { headers: { Authorization: `Bearer ${token}` } });
      if (r.status === 401) { setToken(''); localStorage.removeItem('c2tok'); return; }
      if (r.ok) setAgents(await r.json());
    } catch { /* ignore */ }
  }, [token]);

  const loadCmds = useCallback(async (id: string) => {
    if (!token || !id) return;
    const r = await fetch(`${API}/api/agents/${id}/commands`, { headers: { Authorization: `Bearer ${token}` } });
    if (r.ok) setCmds(await r.json());
  }, [token]);

  useEffect(() => {
    if (!token) return;
    loadAgents();
    const t = setInterval(loadAgents, 8000);
    return () => clearInterval(t);
  }, [loadAgents, token]);

  useEffect(() => {
    if (sel && tab === 'commands') loadCmds(sel);
  }, [sel, tab, loadCmds]);

  useEffect(() => {
    if (!token) return;
    let dead = false;

    const connect = () => {
      const ws = new WebSocket(`${WS_URL}/ws/dashboard?token=${token}`);
      wsRef.current = ws;
      ws.onopen = () => setWsState('on');
      ws.onclose = () => { setWsState('off'); if (!dead) setTimeout(connect, 3000); };
      ws.onerror = () => ws.close();
      ws.onmessage = e => {
        try {
          const msg: WireMsg = JSON.parse(e.data);

          if (msg.type === 'metrics') {
            const m = msg.payload as Metrics;
            setLiveM(p => ({ ...p, [m.agent_id]: m }));
            setCpuHist(p => ({ ...p, [m.agent_id]: [...(p[m.agent_id] ?? []).slice(-29), Math.round(m.cpu_pct)] }));
          }

          if (msg.type === 'agent_up') {
            const p = msg.payload as { id: string; hostname: string };
            addToast({ kind: 'up', hostname: p.hostname, message: 'Agent connected' });
            loadAgents();
          }

          if (msg.type === 'agent_down') {
            const p = msg.payload as { id: string; hostname: string };
            addToast({ kind: 'down', hostname: p.id, message: 'Agent disconnected' });
            loadAgents();
          }

          if (msg.type === 'agent_alert') {
            const a = msg.payload as AlertPayload;
            addToast({ kind: a.kind, hostname: a.hostname, message: a.message });
            if (a.kind === 'offline') {
              setOfflineAgents(p => new Set([...p, a.agent_id]));
            } else if (a.kind === 'recovered') {
              setOfflineAgents(p => { const n = new Set(p); n.delete(a.agent_id); return n; });
            }
            loadAgents();
          }

          if (msg.type === 'cmd_result' && sel) loadCmds(sel);
        } catch { /* ignore */ }
      };
    };
    connect();
    return () => { dead = true; wsRef.current?.close(); };
  }, [token, sel, addToast, loadAgents, loadCmds]);

  const sendCmd = async (type: string, payload = '') => {
    if (!sel || !token) return;
    setSending(true);
    try {
      await fetch(`${API}/api/agents/${sel}/commands`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ type, payload }),
      });
      setTimeout(() => loadCmds(sel), 600);
    } finally { setSending(false); }
  };

  if (!token) return <Login onToken={setToken} />;

  const agent = agents.find(a => a.id === sel) ?? null;
  const m = sel ? (liveM[sel] ?? null) : null;
  const hist = sel ? (cpuHist[sel] ?? []) : [];
  const onlineCount = agents.filter(a => a.online).length;
  const alertCount = offlineAgents.size;

  return (
    <div className="min-h-screen flex flex-col text-white" style={{ background: '#020617', fontFamily: "'Inter','Segoe UI',sans-serif" }}>
      <Toasts toasts={toasts} remove={removeToast} />

      {/* header */}
      <header className="flex items-center justify-between px-6 py-3 border-b shrink-0" style={{ borderColor: '#0f172a' }}>
        <div className="flex items-center gap-3">
          <div className="w-7 h-7 rounded-lg flex items-center justify-center" style={{ background: 'linear-gradient(135deg,#06b6d4,#7c3aed)' }}>
            <div className="w-2.5 h-2.5 rounded-full bg-white/90" />
          </div>
          <span className="font-bold tracking-tight text-sm">C2 Monitor</span>
        </div>
        <div className="flex items-center gap-5 text-xs" style={{ color: '#475569' }}>
          {alertCount > 0 && (
            <div className="flex items-center gap-1.5">
              <AlertBadge count={alertCount} />
              <span className="text-red-400">{alertCount} offline</span>
            </div>
          )}
          <span><span className="text-emerald-400 font-mono">{onlineCount}</span>/{agents.length} online</span>
          <div className="flex items-center gap-1.5">
            <div className={`w-1.5 h-1.5 rounded-full ${wsState === 'on' ? 'bg-emerald-400' : 'bg-amber-400 animate-pulse'}`} />
            <span>{wsState === 'on' ? 'live' : 'reconnecting'}</span>
          </div>
          <button onClick={() => { setToken(''); localStorage.removeItem('c2tok'); }} className="hover:text-slate-400 transition-colors" style={{ color: '#334155' }}>logout</button>
        </div>
      </header>

      <div className="flex flex-1 overflow-hidden">
        {/* sidebar */}
        <aside className="w-64 shrink-0 overflow-y-auto border-r py-3" style={{ borderColor: '#0f172a' }}>
          <p className="px-4 mb-2 text-xs font-medium uppercase tracking-widest" style={{ color: '#334155' }}>Agents</p>
          {agents.length === 0 && <p className="px-4 text-xs" style={{ color: '#1e293b' }}>No agents yet.</p>}
          {agents.map(a => {
            const am = liveM[a.id];
            const ah = cpuHist[a.id] ?? [];
            const active = a.id === sel;
            const isOfflineAlert = offlineAgents.has(a.id);
            const statusColor = isOfflineAlert ? '#ef4444' : a.online ? '#10b981' : '#334155';
            return (
              <button key={a.id} onClick={() => { setSel(a.id); setTab('metrics'); }}
                className="w-full text-left px-4 py-3 transition-all"
                style={{ background: active ? 'rgba(6,182,212,0.07)' : 'transparent', borderLeft: `2px solid ${active ? '#06b6d4' : 'transparent'}` }}>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2 min-w-0">
                    <div className={`w-1.5 h-1.5 rounded-full shrink-0 ${isOfflineAlert ? 'animate-pulse' : ''}`} style={{ background: statusColor }} />
                    <span className="font-mono text-xs truncate text-white">{a.hostname}</span>
                    {isOfflineAlert && <span className="text-xs shrink-0" style={{ color: '#ef4444' }}>!</span>}
                  </div>
                  {ah.length > 1 && <Spark data={ah} color={a.online ? '#06b6d4' : '#334155'} />}
                </div>
                {am && (
                  <div className="mt-2 space-y-1 pl-3.5">
                    <div className="flex items-center gap-1.5">
                      <span className="text-xs w-7 shrink-0" style={{ color: '#475569' }}>cpu</span>
                      <Bar p={am.cpu_pct} color={am.cpu_pct > 80 ? '#ef4444' : am.cpu_pct > 60 ? '#f59e0b' : '#06b6d4'} />
                      <span className="text-xs w-8 text-right font-mono" style={{ color: '#94a3b8' }}>{Math.round(am.cpu_pct)}%</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <span className="text-xs w-7 shrink-0" style={{ color: '#475569' }}>mem</span>
                      <Bar p={pct(am.mem_used, am.mem_total)} color="#7c3aed" />
                      <span className="text-xs w-8 text-right font-mono" style={{ color: '#94a3b8' }}>{pct(am.mem_used, am.mem_total)}%</span>
                    </div>
                  </div>
                )}
              </button>
            );
          })}
        </aside>

        {/* detail */}
        <main className="flex-1 overflow-y-auto">
          {!agent ? (
            <div className="h-full flex flex-col items-center justify-center gap-3" style={{ color: '#1e293b' }}>
              <svg className="w-12 h-12" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M9 3H5a2 2 0 00-2 2v4m6-6h10a2 2 0 012 2v4M9 3v18m0 0h10a2 2 0 002-2V9M9 21H5a2 2 0 01-2-2V9m0 0h18" />
              </svg>
              <p className="text-sm">Select an agent</p>
            </div>
          ) : (
            <div className="p-6 max-w-3xl space-y-6">
              {/* offline banner */}
              {offlineAgents.has(agent.id) && (
                <div className="flex items-center gap-3 px-4 py-3 rounded-xl text-sm" style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)' }}>
                  <span className="text-red-400 font-bold animate-pulse">⚠</span>
                  <span className="text-red-300">Agent has not reported for over 90 seconds</span>
                </div>
              )}

              <div>
                <div className="flex items-center gap-2.5 flex-wrap">
                  <div className="w-2.5 h-2.5 rounded-full" style={{ background: offlineAgents.has(agent.id) ? '#ef4444' : agent.online ? '#10b981' : '#334155' }} />
                  <h2 className="text-2xl font-bold font-mono">{agent.hostname}</h2>
                  <span className="text-xs px-2 py-0.5 rounded-full border font-mono" style={{ borderColor: '#1e293b', color: '#475569' }}>v{agent.version || '?'}</span>
                </div>
                <p className="text-sm mt-1 font-mono" style={{ color: '#475569' }}>{agent.ip} · {agent.os}/{agent.arch}</p>
                <p className="text-xs mt-0.5" style={{ color: '#1e293b' }}>Last seen {ago(agent.last_seen)}</p>
              </div>

              {/* tabs */}
              <div className="flex border-b" style={{ borderColor: '#0f172a' }}>
                {(['metrics', 'commands', 'events'] as const).map(t => (
                  <button key={t} onClick={() => setTab(t)}
                    className="px-4 py-2 text-xs font-medium capitalize border-b-2 -mb-px transition-colors"
                    style={{ borderColor: tab === t ? '#06b6d4' : 'transparent', color: tab === t ? '#06b6d4' : '#334155' }}>
                    {t}
                  </button>
                ))}
              </div>

              {/* metrics */}
              {tab === 'metrics' && (
                <div className="space-y-3">
                  {!m && <p className="text-xs" style={{ color: '#334155' }}>Waiting for first beacon…</p>}
                  {m && (
                    <div className="grid grid-cols-2 gap-3">
                      <div className="col-span-2 rounded-xl p-4 space-y-3" style={{ background: '#0f172a', border: '1px solid #1e293b' }}>
                        <div className="flex justify-between text-xs">
                          <span style={{ color: '#475569' }}>CPU</span>
                          <span className="font-mono text-white">{m.cpu_pct.toFixed(1)}%</span>
                        </div>
                        <Bar p={m.cpu_pct} color={m.cpu_pct > 80 ? '#ef4444' : m.cpu_pct > 60 ? '#f59e0b' : '#06b6d4'} />
                        {hist.length > 1 && (
                          <svg width="100%" height="36">
                            {(() => {
                              const max = Math.max(...hist, 1);
                              const pts = hist.map((v, i) => `${(i / (hist.length - 1)) * 100}%,${32 - (v / max) * 28}`).join(' ');
                              return <>
                                <polyline fill="none" stroke="#06b6d4" strokeWidth="1.5" strokeLinejoin="round" points={pts} opacity={0.4} />
                                <circle cx={`${100}%`} cy={32 - (hist[hist.length-1] / max) * 28} r="2.5" fill="#06b6d4" />
                              </>;
                            })()}
                          </svg>
                        )}
                      </div>
                      <div className="rounded-xl p-4 space-y-2" style={{ background: '#0f172a', border: '1px solid #1e293b' }}>
                        <div className="flex justify-between text-xs"><span style={{ color: '#475569' }}>Memory</span><span className="font-mono text-white">{pct(m.mem_used, m.mem_total)}%</span></div>
                        <Bar p={pct(m.mem_used, m.mem_total)} color="#7c3aed" />
                        <p className="text-xs font-mono" style={{ color: '#334155' }}>{fmtBytes(m.mem_used)} / {fmtBytes(m.mem_total)}</p>
                      </div>
                      <div className="rounded-xl p-4 space-y-2" style={{ background: '#0f172a', border: '1px solid #1e293b' }}>
                        <div className="flex justify-between text-xs"><span style={{ color: '#475569' }}>Disk</span><span className="font-mono text-white">{pct(m.disk_used, m.disk_total)}%</span></div>
                        <Bar p={pct(m.disk_used, m.disk_total)} color="#10b981" />
                        <p className="text-xs font-mono" style={{ color: '#334155' }}>{fmtBytes(m.disk_used)} / {fmtBytes(m.disk_total)}</p>
                      </div>
                      <div className="rounded-xl p-4" style={{ background: '#0f172a', border: '1px solid #1e293b' }}>
                        <p className="text-xs mb-2" style={{ color: '#475569' }}>Network</p>
                        <p className="font-mono text-xs text-white">↓ {fmtBytes(m.net_rx)}</p>
                        <p className="font-mono text-xs mt-1" style={{ color: '#475569' }}>↑ {fmtBytes(m.net_tx)}</p>
                      </div>
                      <div className="rounded-xl p-4 flex flex-col justify-between" style={{ background: '#0f172a', border: '1px solid #1e293b' }}>
                        <p className="text-xs" style={{ color: '#475569' }}>Processes</p>
                        <p className="text-3xl font-bold font-mono text-white mt-2">{m.proc_count}</p>
                        <p className="text-xs mt-1" style={{ color: '#334155' }}>running · {ago(m.recorded_at)}</p>
                      </div>
                    </div>
                  )}
                </div>
              )}

              {/* commands */}
              {tab === 'commands' && (
                <div className="space-y-4">
                  <div className="grid grid-cols-3 gap-2">
                    {CMDS.map(c => (
                      <button key={c.type} onClick={() => sendCmd(c.type, c.payload)} disabled={sending}
                        className="flex flex-col items-center gap-1.5 p-3 rounded-xl text-xs transition-all disabled:opacity-40"
                        style={{ background: '#0f172a', border: '1px solid #1e293b', color: '#94a3b8' }}
                        onMouseEnter={e => (e.currentTarget.style.borderColor = '#06b6d4')}
                        onMouseLeave={e => (e.currentTarget.style.borderColor = '#1e293b')}>
                        <span className="text-xl">{c.icon}</span>
                        <span>{c.label}</span>
                      </button>
                    ))}
                  </div>
                  <div className="flex items-center gap-2 p-3 rounded-xl" style={{ background: '#0f172a', border: '1px solid #1e293b' }}>
                    <span className="text-xs shrink-0" style={{ color: '#475569' }}>Beacon interval</span>
                    <input type="number" min={5} max={3600} value={beaconSecs} onChange={e => setBeaconSecs(e.target.value)}
                      className="w-20 rounded-lg px-2 py-1 text-xs font-mono text-white border outline-none"
                      style={{ background: '#020617', borderColor: '#1e293b' }} />
                    <span className="text-xs" style={{ color: '#334155' }}>sec</span>
                    <button onClick={() => sendCmd('set_beacon', beaconSecs)} disabled={sending}
                      className="ml-auto px-3 py-1 rounded-lg text-xs font-medium text-white disabled:opacity-40"
                      style={{ background: 'linear-gradient(135deg,#7c3aed,#06b6d4)' }}>
                      Apply
                    </button>
                  </div>
                  <div className="space-y-2 max-h-72 overflow-y-auto">
                    {cmds.length === 0 && <p className="text-xs" style={{ color: '#1e293b' }}>No commands yet.</p>}
                    {cmds.map(c => (
                      <div key={c.id} className="rounded-xl p-3" style={{ background: '#0f172a', border: '1px solid #1e293b' }}>
                        <div className="flex items-center justify-between mb-1">
                          <span className="text-xs font-mono" style={{ color: '#06b6d4' }}>{c.type}</span>
                          <span className={`text-xs ${c.status === 'ok' ? 'text-emerald-400' : c.status === 'pending' ? 'text-amber-400' : 'text-red-400'}`}>{c.status}</span>
                        </div>
                        <p className="text-xs" style={{ color: '#334155' }}>{ago(c.created_at)}</p>
                        {c.result && (
                          <pre className="mt-2 text-xs rounded-lg p-2 overflow-x-auto whitespace-pre-wrap break-all leading-relaxed" style={{ background: '#020617', color: '#94a3b8' }}>
                            {(() => { try { return JSON.stringify(JSON.parse(c.result), null, 2); } catch { return c.result; } })()}
                          </pre>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* events */}
              {tab === 'events' && <EventsTab agentID={agent.id} token={token} />}
            </div>
          )}
        </main>
      </div>
    </div>
  );
}

function EventsTab({ agentID, token }: { agentID: string; token: string }) {
  const [events, setEvents] = useState<{ agent_id: string; kind: string; message: string; created_at: string }[]>([]);
  const load = () => fetch(`${API}/api/agents/${agentID}/events`, { headers: { Authorization: `Bearer ${token}` } })
    .then(r => r.json()).then(setEvents).catch(() => {});
  useEffect(() => { load(); const t = setInterval(load, 10000); return () => clearInterval(t); }, [agentID, token]);
  const a = (iso: string) => { const d = Math.floor((Date.now() - new Date(iso).getTime()) / 1000); if (d < 60) return `${d}s`; if (d < 3600) return `${Math.floor(d/60)}m`; return `${Math.floor(d/3600)}h`; };
  const kindColor = (k: string) => k === 'connect' ? '#10b981' : k === 'disconnect' ? '#ef4444' : k.includes('alert') ? '#f59e0b' : '#06b6d4';
  return (
    <div className="space-y-1.5 max-h-96 overflow-y-auto">
      {events.length === 0 && <p className="text-xs" style={{ color: '#1e293b' }}>No events.</p>}
      {events.map((e, i) => (
        <div key={i} className="flex items-start gap-3 text-xs p-2.5 rounded-lg" style={{ background: '#0f172a' }}>
          <span className="shrink-0 font-mono w-7 text-right" style={{ color: '#334155' }}>{a(e.created_at)}</span>
          <span className="shrink-0 font-medium" style={{ color: kindColor(e.kind) }}>{e.kind}</span>
          <span className="font-mono truncate" style={{ color: '#475569' }}>{e.message}</span>
        </div>
      ))}
    </div>
  );
}
