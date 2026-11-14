import type { LiveData } from '../hooks';
import { fmt, shortId } from '../format';
import { Card, Empty, TableWrap } from '../ui';

export default function Ledger({ data }: { data: LiveData }) {
  const rows = data.ledger;
  return (
    <Card title="Audit ledger" note="Hash-chained entries from the backend. Hashes are shortened here; hover to see the full value." flush>
      {rows.length ? (
        <TableWrap><table aria-label="Audit ledger entries"><thead><tr><th>Seq</th><th>Event reference</th><th>Previous hash</th><th>Current hash</th><th>Timestamp</th><th>Checkpoint</th></tr></thead><tbody>
          {rows.map((e) => <tr key={e.seq}><td>{e.seq}</td><td className="mono">{e.eventRef}</td><td className="mono" title={e.prevHash}>{shortId(e.prevHash, 16)}</td><td className="mono" title={e.hash}>{shortId(e.hash, 16)}</td><td>{fmt(e.timestamp)}</td><td>{e.checkpointSignatureStatus}</td></tr>)}
        </tbody></table></TableWrap>
      ) : <Empty>No audit ledger entries are available from the backend.</Empty>}
    </Card>
  );
}
