import type { ReactNode } from 'react';

/** Every screen starts with the same header: title, one-line purpose, optional actions. */
export function PageHeader({ title, description, actions }: { title: string; description: string; actions?: ReactNode }) {
  return (
    <div className="page-header">
      <div><h1>{title}</h1><p>{description}</p></div>
      {actions && <div className="page-actions">{actions}</div>}
    </div>
  );
}

export function Card({ title, note, actions, children, flush }: { title?: string; note?: string; actions?: ReactNode; children: ReactNode; flush?: boolean }) {
  return (
    <section className="card">
      {(title || actions) && (
        <header className="card-head">
          <div>{title && <h2>{title}</h2>}{note && <p>{note}</p>}</div>
          {actions}
        </header>
      )}
      <div className={flush ? 'card-body flush' : 'card-body'}>{children}</div>
    </section>
  );
}

export function Metric({ label, value, hint }: { label: string; value: number | string; hint?: string }) {
  return <div className="metric"><div className="label">{label}</div><div className="value">{value}</div>{hint && <div className="hint">{hint}</div>}</div>;
}

const LABELS: Record<string, string> = { ACCEPT: 'Accepted', PROVISIONAL_ESCROW: 'Escrow', QUARANTINE: 'Quarantined', PENDING: 'Pending', CORROBORATED: 'Corroborated', EXPIRED_QUARANTINED: 'Expired' };
const TONES: Record<string, string> = { ACCEPT: 'accept', CORROBORATED: 'accept', PROVISIONAL_ESCROW: 'escrow', PENDING: 'escrow', QUARANTINE: 'quarantine', EXPIRED_QUARANTINED: 'quarantine' };

export function Badge({ state }: { state: string }) {
  return <span className={`badge ${TONES[state] ?? ''}`}>{LABELS[state] ?? state}</span>;
}

export function Banner({ tone, children }: { tone: 'error' | 'info'; children: ReactNode }) {
  return <div className={`banner ${tone}`} role={tone === 'error' ? 'alert' : 'status'}>{children}</div>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>;
}

/** Scroll container so wide tables never break the page layout. */
export function TableWrap({ children }: { children: ReactNode }) {
  return <div className="table-wrap">{children}</div>;
}

export function Button({ variant = 'default', ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'default' | 'primary' }) {
  return <button {...props} className={`btn ${variant === 'primary' ? 'primary' : ''} ${props.className ?? ''}`} />;
}

/** Link that looks like a button; opens another app (e.g. the simulator) in a new tab. */
export function LinkButton({ href, variant = 'default', children }: { href: string; variant?: 'default' | 'primary'; children: ReactNode }) {
  return <a className={`btn ${variant === 'primary' ? 'primary' : ''}`} href={href} target="_blank" rel="noopener noreferrer">{children}</a>;
}

/** Label/value pairs for a detail card. */
export function Facts({ items }: { items: { k: string; v: ReactNode }[] }) {
  return <dl className="facts">{items.map(({ k, v }) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}</dl>;
}
