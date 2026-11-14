import { useState } from 'react';

// Pointy-top hexagons: a centre cell and its six k=1 neighbours.
const S = 40;
const CX = 170;
const CY = 150;
const STEP = Math.sqrt(3) * S;
const rad = (deg: number) => (Math.PI / 180) * deg;
const corners = (cx: number, cy: number) =>
  Array.from({ length: 6 }, (_, k) => `${(cx + S * Math.cos(rad(60 * k + 30))).toFixed(1)},${(cy + S * Math.sin(rad(60 * k + 30))).toFixed(1)}`).join(' ');
const RING = Array.from({ length: 6 }, (_, k) => ({ x: CX + STEP * Math.cos(rad(60 * k)), y: CY + STEP * Math.sin(rad(60 * k)) }));

// The figure carries its own paint, so it reads the same wherever it is rendered.
const INK = '#0e0e0e';
const MUTED = '#6a655c';
const MONO = 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace';
const PAINT = {
  wet: { fill: '#eef3ff', stroke: '#0b5cff', dash: undefined, text: INK },
  dry: { fill: '#ffffff', stroke: MUTED, dash: '4 3', text: MUTED },
  hold: { fill: '#fff8ec', stroke: '#a35c00', dash: undefined, text: INK },
  ok: { fill: '#f1faf4', stroke: '#0a7a3d', dash: undefined, text: INK },
};

const PRESETS = {
  real: { label: 'Real storm', wet: [true, true, true, true, false, false] },
  staged: { label: 'Staged claim', wet: [false, false, false, false, false, false] },
} as const;

/** Interactive version of the H3 tie-breaker: an escrow is released by radar, or by at least 2 raining neighbour cells. */
export default function HexExplainer() {
  const [wet, setWet] = useState<boolean[]>([...PRESETS.real.wet]);
  const [radar, setRadar] = useState(false);
  const raining = wet.filter(Boolean).length;
  const byNeighbours = raining >= 2;
  const released = radar || byNeighbours;
  const toggle = (i: number) => setWet((cur) => cur.map((w, j) => (j === i ? !w : w)));
  const verdict = released
    ? `Released: ACCEPT · ESCROW_CORROBORATED, by ${radar ? 'later live radar' : `${raining} raining neighbour cells`}.`
    : `Held: ${raining} of 2 needed neighbour cells are raining. After 15 minutes it expires: QUARANTINE · ESCROW_EXPIRED_UNCORROBORATED.`;

  return (
    <figure className="hexfig">
      <div className="segments" role="group" aria-label="Presets">
        {(Object.keys(PRESETS) as (keyof typeof PRESETS)[]).map((k) => (
          <button key={k} onClick={() => { setWet([...PRESETS[k].wet]); setRadar(false); }}>{PRESETS[k].label}</button>
        ))}
        <button aria-pressed={radar} className={radar ? 'on' : ''} onClick={() => setRadar((r) => !r)}>Radar recovers</button>
      </div>
      <svg viewBox="0 0 340 300" role="group" aria-label="An escrowed H3 cell and its six neighbours">
        {RING.map((n, i) => {
          const p = PAINT[wet[i] ? 'wet' : 'dry'];
          return (
            <g key={i} className="hx" role="button" tabIndex={0} aria-pressed={wet[i]}
              aria-label={`Neighbour ${i + 1}: ${wet[i] ? 'raining' : 'dry'}`} onClick={() => toggle(i)}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggle(i); } }}>
              <polygon points={corners(n.x, n.y)} fill={p.fill} stroke={p.stroke} strokeWidth={1.5} strokeDasharray={p.dash} />
              <text x={n.x} y={n.y + 4} textAnchor="middle" fontSize={11} fontFamily={MONO} fill={p.text}>{wet[i] ? 'raining' : 'dry'}</text>
            </g>
          );
        })}
        <g className="hx center">
          <polygon points={corners(CX, CY)} fill={PAINT[released ? 'ok' : 'hold'].fill} stroke={PAINT[released ? 'ok' : 'hold'].stroke} strokeWidth={2.5} />
          <text x={CX} y={CY - 2} textAnchor="middle" fontSize={13} fontWeight={700} fontFamily={MONO} fill={INK}>{released ? 'released' : 'escrow'}</text>
          <text x={CX} y={CY + 15} textAnchor="middle" fontSize={10} fontFamily={MONO} fill={INK}>{released ? 'ACCEPT' : '15 min TTL'}</text>
        </g>
      </svg>
      <figcaption aria-live="polite">{verdict}</figcaption>
    </figure>
  );
}
