import type { LiveData } from '../hooks';
import { useCountdown } from '../hooks';
import { fmt, shortId } from '../format';
import type { EscrowRecord } from '../types';
import { Badge, Card, Empty, TableWrap } from '../ui';

function Remaining({ escrow }: { escrow: EscrowRecord }) {
  const left = useCountdown(escrow.expiresAt);
  return <>{escrow.state === 'PENDING' ? left : '—'}</>;
}

export default function Escrows({ data }: { data: LiveData }) {
  const rows = data.escrows;
  return (
    <Card title="Escrow records" note="Held readings wait for corroboration. Time remaining counts down to each stored expiry." flush>
      {rows.length ? (
        <TableWrap><table aria-label="Escrow records"><thead><tr><th>Escrow</th><th>Device</th><th>H3 cell</th><th>State</th><th>Created</th><th>Expires</th><th>Remaining</th><th>Corroborated</th></tr></thead><tbody>
          {rows.map((e) => (
            <tr key={e.id}><td className="mono" title={e.id}>{shortId(e.id)}</td><td className="mono">{e.deviceId}</td><td className="mono">{e.h3Index}</td><td><Badge state={e.state} /></td>
              <td>{fmt(e.createdAt)}</td><td>{fmt(e.expiresAt)}</td><td><Remaining escrow={e} /></td><td>{fmt(e.corroboratedAt)}</td></tr>
          ))}
        </tbody></table></TableWrap>
      ) : <Empty>No escrow records returned.</Empty>}
    </Card>
  );
}
