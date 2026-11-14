import { useEffect, useMemo } from 'react';
import Wordmark from '../brand/Wordmark';
import { buildHash, setQuery, useRoute } from '../router';
import { Banner, Empty } from '../ui';
import { Blocks } from './Blocks';
import { ARTICLES, articleUrl, searchDocs, SECTIONS, sectionOf, type Section } from './content';

const STATS = [
  ['37', 'H3 cells in the virtual city'], ['210', 'enrolled devices'], ['12', 'scenarios, limits included'],
  ['6', 'neighbours per cell (k=1)'], ['7', 'checks on every reading'], ['15 min', 'escrow lifetime'],
];

function Preamble() {
  return (
    <>
      <section className="hero">
        <Wordmark className="hero-mark" />
        <p className="hero-tag">Urban water verification</p>
        <p className="hero-name"><b>Varuna</b> (वरुण), the Vedic guardian of the waters, and <b>setu</b> (सेतु), a bridge.</p>
      </section>
      <div className="prose serif">
        <p className="lead">Rain falls on a city, and thousands of rooftops can catch it. Each roof has a device that reports how many litres it collected. Water that is credited, shared or relied upon should be water that was really collected.</p>
        <p>VarunaSetu is a physics-informed, spatially aware urban water intelligence prototype. Of every claim it asks three things. Is it physically possible for this roof, under this radar? Do the devices on the same few blocks agree? And when the evidence is divided, what do the neighbouring H3 cells say?</p>
        <p>It holds what it cannot yet decide, rejects what the physics forbids, writes every verdict into a hash-chained ledger, and says plainly where it can still be fooled.</p>
        <p>The commitments that follow from this are set out in the <a href="#/constitution">Constitution</a>. How they are kept is in <a href="#/how-it-works">How it works</a>.</p>
      </div>
      <dl className="stats">
        {STATS.map(([n, l]) => <div key={l}><dt>{n}</dt><dd>{l}</dd></div>)}
      </dl>
      <div className="cta">
        <a className="btn primary" href="#/how-it-works">How it works →</a>
        <a className="btn" href="#/constitution">Constitution</a>
        <a className="btn" href="#/documentation">Documentation</a>
        <a className="btn" href="#/story">Run a story</a>
      </div>
    </>
  );
}

function Constitution({ target }: { target?: string }) {
  return (
    <>
      <p className="serif intro">These articles are drawn from the project’s own documents. They state what the system holds itself to, and what it refuses to claim.</p>
      <ol className="articles">
        {ARTICLES.map((a) => (
          <li key={a.id} id={`article-${a.id}`} className={a.id === target ? 'article target' : 'article'} aria-current={a.id === target ? 'true' : undefined}>
            <span className="numeral" aria-hidden="true">{a.numeral}</span>
            <div>
              <h2>{a.title}</h2>
              <p>{a.text}</p>
              <a className="src" href={articleUrl(a)} target="_blank" rel="noopener noreferrer">{a.source} ↗</a>
            </div>
          </li>
        ))}
      </ol>
    </>
  );
}

function PageView({ section, pageId }: { section: Section; pageId?: string }) {
  const found = section.pages.findIndex((p) => p.id === pageId);
  const index = Math.max(0, found);
  const page = section.pages[index];
  const prev = section.pages[index - 1];
  const next = section.pages[index + 1];
  return (
    <>
      {pageId && found < 0 && <Banner tone="info">There is no page “{pageId}” in {section.nav}. Showing {page.title}.</Banner>}
      <header className="page-title"><span className="kicker">{section.nav}</span><h2>{page.title}</h2><p>{page.summary}</p></header>
      <Blocks blocks={page.blocks} />
      <nav className="docs-pager" aria-label="Page navigation">
        {prev ? <a href={buildHash([section.id, prev.id])}>← {prev.title}</a> : <span />}
        {next ? <a href={buildHash([section.id, next.id])}>{next.title} →</a> : <span />}
      </nav>
    </>
  );
}

function Results({ q }: { q: string }) {
  const hits = useMemo(() => searchDocs(q), [q]);
  return (
    <>
      <header className="page-title"><span className="kicker">Search</span><h2>{hits.length} {hits.length === 1 ? 'result' : 'results'} for “{q}”</h2></header>
      {hits.length ? (
        <ul className="results">
          {hits.map((h) => (
            <li key={h.path.join('/')}><a href={buildHash(h.path)}><strong>{h.title}</strong><span className="kicker">{h.where}</span></a><p>{h.snippet}</p></li>
          ))}
        </ul>
      ) : <Empty>Nothing matches. Try a reason code, a term from the glossary or an endpoint.</Empty>}
    </>
  );
}

/** The documentation body: /docs/#/:section/:page, with ?q= search and ?at= heading anchors. */
export default function Docs() {
  const { segments, query } = useRoute();
  const unknown = Boolean(segments[0]) && !SECTIONS.some((s) => s.id === segments[0]);
  const section = sectionOf(segments[0]);
  const param = segments[1];
  const q = query.get('q')?.trim() ?? '';
  const at = query.get('at');

  useEffect(() => {
    const id = at ?? (section.id === 'constitution' && param ? `article-${param}` : 'docs-top');
    document.getElementById(id)?.scrollIntoView?.({ block: 'start' });
  }, [at, section.id, param, q]);

  return (
    <div className="docs" id="docs-top">
      <nav className="docs-nav" aria-label="Documentation">
        <input type="search" aria-label="Search documentation" placeholder="Search the docs" value={query.get('q') ?? ''} onChange={(e) => setQuery({ q: e.target.value || null })} />
        {SECTIONS.map((s) => {
          const active = s.id === section.id;
          const here = (id: string) => (active && (param ?? s.pages[0]?.id) === id ? 'page' : undefined);
          return (
            <div key={s.id} className="docs-group">
              <a href={buildHash([s.id])} className={active && !q ? 'on' : ''} aria-current={active && !param && !q ? 'page' : undefined}>{s.nav}</a>
              {active && s.pages.length > 0 && (
                <ul>{s.pages.map((pg) => <li key={pg.id}><a href={buildHash([s.id, pg.id])} aria-current={here(pg.id)}>{pg.title}</a></li>)}</ul>
              )}
              {active && s.id === 'constitution' && (
                <ul>{ARTICLES.map((a) => <li key={a.id}><a href={buildHash([s.id, a.id])} aria-current={param === a.id ? 'page' : undefined}>{a.numeral}. {a.title}</a></li>)}</ul>
              )}
            </div>
          );
        })}
      </nav>
      <article className="docs-article">
        {unknown && <Banner tone="info">There is no section “{segments[0]}”. Showing the Preamble.</Banner>}
        {q ? <Results q={q} />
          : section.id === 'preamble' ? <Preamble />
          : section.id === 'constitution' ? <Constitution target={param} />
          : <PageView section={section} pageId={param} />}
      </article>
    </div>
  );
}
