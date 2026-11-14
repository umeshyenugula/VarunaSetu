import { buildHash, useRoute } from '../router';
import { TableWrap } from '../ui';
import { slug, type Block } from './content';
import HexExplainer from './HexExplainer';

const TOKEN = /(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\))/g;
const LINK = /^\[([^\]]+)\]\(([^)]+)\)$/;

/** Inline markup used in the content: `code`, **bold** and [links](#/route or https://…). */
export function Inline({ x }: { x: string }) {
  return (
    <>
      {x.split(TOKEN).map((part, i) => {
        if (part.length > 1 && part.startsWith('`') && part.endsWith('`')) return <code key={i}>{part.slice(1, -1)}</code>;
        if (part.startsWith('**') && part.endsWith('**')) return <strong key={i}>{part.slice(2, -2)}</strong>;
        const link = LINK.exec(part);
        if (link) {
          const [, text, href] = link;
          if (href.startsWith('#/')) return <a key={i} href={href}>{text}</a>;
          if (href.startsWith('https://')) return <a key={i} href={href} target="_blank" rel="noopener noreferrer">{text}</a>;
          return text;
        }
        return part;
      })}
    </>
  );
}

export function Blocks({ blocks }: { blocks: Block[] }) {
  const { segments } = useRoute();
  return (
    <div className="prose">
      {blocks.map((b, i) => {
        switch (b.t) {
          case 'p': return <p key={i}><Inline x={b.x} /></p>;
          case 'h': {
            const id = slug(b.x);
            return <h3 key={i} id={id}>{b.x}<a className="anchor" href={buildHash(segments, { at: id })} aria-label={`Link to ${b.x}`}>#</a></h3>;
          }
          case 'ul': return <ul key={i}>{b.x.map((li, j) => <li key={j}><Inline x={li} /></li>)}</ul>;
          case 'ol': return <ol key={i}>{b.x.map((li, j) => <li key={j}><Inline x={li} /></li>)}</ol>;
          case 'table':
            return (
              <TableWrap key={i}>
                <table>
                  <thead><tr>{b.head.map((c, j) => <th key={j} scope="col">{c}</th>)}</tr></thead>
                  <tbody>{b.rows.map((row, r) => <tr key={r}>{row.map((c, j) => <td key={j}><Inline x={c} /></td>)}</tr>)}</tbody>
                </table>
              </TableWrap>
            );
          case 'code': return <pre key={i} className="code"><code>{b.x}</code></pre>;
          case 'note': return <div key={i} className={b.warn ? 'note warn' : 'note'}><Inline x={b.x} /></div>;
          case 'steps':
            return (
              <ol key={i} className="pipe">
                {b.x.map((s, j) => (
                  <li key={j}><span className="num">{j + 1}</span><div><strong>{s.title}</strong><p><Inline x={s.body} /></p></div></li>
                ))}
              </ol>
            );
          case 'hex': return <HexExplainer key={i} />;
        }
      })}
    </div>
  );
}
