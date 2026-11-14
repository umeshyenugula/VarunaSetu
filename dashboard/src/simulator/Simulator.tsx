import { useEffect, useRef, useState } from 'react';
import { backend, onApiCall, resolvedUrl, simulatorApi, submitSignedTelemetry, type ApiCall, type ProvisionDevice, type Verdict } from './api';
import { Badge, Button, Card, Empty, TableWrap } from '../ui';
import { expectedLitres, plan, STORIES, type Planned, type Story, type StoryId } from './story';

type Status = 'idle' | 'active' | 'done' | 'error';
type Row = { p: Planned; http?: number; verdict?: Verdict['decision']; error?: string };

const CHAPTERS = [
  { key: 'connect', title: 'Reach the backend', plain: 'The dashboard asks the API on port 8080 if it is alive.' },
  { key: 'city', title: 'Set up the city', plain: 'Simulated buildings are enrolled with secret keys and placed on H3 map cells.' },
  { key: 'rain', title: 'Rain falls', plain: 'Weather radar reports how many millimetres fell.' },
  { key: 'report', title: 'Roofs report', plain: 'Each device signs its reading and sends it to the backend.' },
  { key: 'verdict', title: 'Backend decides', plain: 'Radar, physics and neighbours are compared. Accept, hold or quarantine.' },
  { key: 'record', title: 'It is recorded', plain: 'Decisions are stored and chained in the audit ledger.' },
] as const;
type Key = typeof CHAPTERS[number]['key'];

const pause = (ms: number) => new Promise((r) => setTimeout(r, ms));
const idle = (): Record<Key, Status> => ({ connect: 'idle', city: 'idle', rain: 'idle', report: 'idle', verdict: 'idle', record: 'idle' });

export default function Simulator() {
  const [storyId, setStoryId] = useState<StoryId>('honest');
  const [status, setStatus] = useState<Record<Key, Status>>(idle);
  const [notes, setNotes] = useState<Partial<Record<Key, string>>>({});
  const [rows, setRows] = useState<Row[]>([]);
  const [calls, setCalls] = useState<ApiCall[]>([]);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState('');
  const [outcome, setOutcome] = useState('');
  const alive = useRef(true);
  const story = STORIES.find((s) => s.id === storyId) as Story;

  useEffect(() => { alive.current = true; const off = onApiCall((c) => setCalls((l) => [c, ...l].slice(0, 30))); return () => { alive.current = false; off(); }; }, []);

  const mark = (key: Key, s: Status, note?: string) => {
    setStatus((cur) => ({ ...cur, [key]: s }));
    if (note !== undefined) setNotes((cur) => ({ ...cur, [key]: note }));
  };

  async function run() {
    setRunning(true); setError(''); setOutcome(''); setRows([]); setStatus(idle()); setNotes({});
    let current: Key = 'connect';
    try {
      mark('connect', 'active');
      const t0 = performance.now();
      await backend.health();
      mark('connect', 'done', `Alive at ${resolvedUrl('/healthz')} (${Math.round(performance.now() - t0)} ms)`);

      current = 'city'; mark('city', 'active');
      const city = await simulatorApi.provision();
      const devices: ProvisionDevice[] = city.devices;
      if (!devices.length) throw new Error('The backend enrolled no devices.');
      mark('city', 'done', `${devices.length} new devices on ${city.cells.length} H3 cells. Earlier signed readings remain immutable.`);

      current = 'rain'; mark('rain', 'active'); await pause(500);
      const planned = plan(story, devices);
      mark('rain', 'done', `Radar: ${story.radarMm} mm. ${story.plain}`);

      current = 'report'; mark('report', 'active');
      const out: Row[] = planned.map((p) => ({ p }));
      setRows([...out]);
      for (let i = 0; i < planned.length; i++) {
        await pause(350);
        try {
          const res = await submitSignedTelemetry(planned[i].device, planned[i].payload);
          out[i] = { ...out[i], http: 201, verdict: res.decision };
        } catch (e) {
          out[i] = { ...out[i], error: e instanceof Error ? e.message : String(e) };
        }
        if (!alive.current) return;
        setRows([...out]);
      }
      mark('report', 'done', `${planned.length} signed readings sent to POST /telemetry/readings.`);

      current = 'verdict'; mark('verdict', 'active'); await pause(400);
      const got = out.filter((r) => r.verdict);
      const count = (s: string) => got.filter((r) => r.verdict?.State === s).length;
      const summary = `${count('ACCEPT')} accepted, ${count('PROVISIONAL_ESCROW')} held, ${count('QUARANTINE')} quarantined` + (got.length < out.length ? `, ${out.length - got.length} rejected by the API` : '');
      mark('verdict', got.length ? 'done' : 'error', summary);
      setOutcome(`${story.expect} Backend said: ${summary}.`);

      current = 'record'; mark('record', 'active');
      const stats = await backend.analytics();
      const ledger = await backend.ledger().catch(() => null);
      mark('record', 'done', `${stats.totalReadings} readings stored in total. Audit ledger entries: ${ledger ? ledger.length : 'unavailable'}.`);
    } catch (e) {
      mark(current, 'error');
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (alive.current) setRunning(false);
    }
  }

  const doneCount = CHAPTERS.filter((c) => status[c.key] === 'done').length;
  return (
    <>
      <Card title="Scenario" note="Pick what happens, then run it. Nothing is faked: the backend returns every verdict.">
        <div className="segments" role="radiogroup" aria-label="Story">
          {STORIES.map((s) => (
            <button key={s.id} role="radio" aria-checked={s.id === storyId} className={s.id === storyId ? 'on' : ''} disabled={running} onClick={() => setStoryId(s.id)}>{s.title}</button>
          ))}
        </div>
        <p className="lede">{story.plain}</p>
        <p className="muted">Expected outcome: {story.expect}</p>
        <Button variant="primary" onClick={run} disabled={running}>{running ? 'Running…' : 'Run story'}</Button>
      </Card>

      {error && <div className="banner error" role="alert">{error}</div>}

      <Card title="Sequence" note={`${doneCount} of ${CHAPTERS.length} steps complete`}>
        <div className="progress" aria-hidden="true">{CHAPTERS.map((c) => <span key={c.key} className={status[c.key]} />)}</div>
        <ol className="chapters">
          {CHAPTERS.map((c, i) => (
            <li key={c.key} className={`chapter ${status[c.key]}`}>
              <span className="num">{i + 1}</span>
              <div className="body">
                <div className="head"><strong>{c.title}</strong><span className="state">{status[c.key] === 'idle' ? '' : status[c.key]}</span></div>
                <p className="muted">{c.plain}</p>
                {notes[c.key] && <p className="note">{notes[c.key]}</p>}
                {c.key === 'report' && rows.length > 0 && <Readings rows={rows} radar={story.radarMm} />}
              </div>
            </li>
          ))}
        </ol>
        {outcome && <p className="outcome">{outcome}</p>}
      </Card>

      <Card title="Requests sent" note="Every call the browser makes, with the exact address it went to." flush>
        {calls.length ? (
          <TableWrap><table aria-label="Request log"><thead><tr><th>Method</th><th>Backend URL</th><th>Via</th><th>Status</th><th>ms</th></tr></thead>
            <tbody>{calls.map((c, i) => <tr key={i} className={c.ok ? '' : 'bad'}><td className="mono">{c.method}</td><td className="mono">{c.url}</td><td className="mono muted">{c.via ? 'dev proxy' : 'direct'}</td><td>{c.status || 'no reply'}</td><td>{c.ms}</td></tr>)}</tbody></table></TableWrap>
        ) : <Empty>Nothing sent yet. Run a story.</Empty>}
      </Card>
    </>
  );
}

function Readings({ rows, radar }: { rows: Row[]; radar: number }) {
  return <TableWrap><table aria-label="Device readings"><thead><tr><th>Device</th><th>Claim</th><th>Fair claim at {radar} mm</th><th>Verdict</th><th>Reason</th></tr></thead><tbody>
    {rows.map((r) => <tr key={r.p.device.deviceId}>
      <td className="mono">{r.p.device.deviceId}</td>
      <td>{r.p.payload.claimed_litres.toLocaleString()} L{r.p.suspicious ? ' ⚑' : ''}</td>
      <td>{Math.round(expectedLitres(radar, r.p.device)).toLocaleString()} L</td>
      <td>{r.verdict ? <Badge state={r.verdict.State} /> : r.error ? <span className="badge quarantine">Rejected</span> : '…'}</td>
      <td className="mono">{r.verdict?.Reason ?? r.error ?? ''}</td></tr>)}
  </tbody></table></TableWrap>;
}
