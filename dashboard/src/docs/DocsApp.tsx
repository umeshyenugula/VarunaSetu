import { useEffect } from 'react';
import Wordmark from '../brand/Wordmark';
import { dashboardUrl, simulatorUrl } from '../config';
import { buildHash, useRoute } from '../router';
import { PageHeader } from '../ui';
import Docs from './Docs';
import { DEEPWIKI, GITHUB, sectionOf } from './content';

/**
 * The documentation is a page of its own (/docs/), not a screen of the dashboard: its own masthead,
 * its own routes (#/:section/:page) and no backend calls, so it works with nothing else running.
 */
export default function DocsApp() {
  const { segments } = useRoute();
  const section = sectionOf(segments[0]);
  useEffect(() => { document.title = `${section.title} · VarunaSetu Docs`; }, [section.title]);

  return (
    <div className="docs-shell">
      <header className="masthead">
        <a className="mast-brand" href={buildHash(['preamble'])} aria-label="VarunaSetu documentation, home"><Wordmark className="wordmark" /></a>
        <span className="mast-tag">Documentation</span>
        <nav className="mast-links" aria-label="Elsewhere">
          <a className="btn primary" href={dashboardUrl()}>← Dashboard</a>
          <a className="btn" href={simulatorUrl()} target="_blank" rel="noopener noreferrer">City simulator ↗</a>
          <a className="btn" href={GITHUB} target="_blank" rel="noopener noreferrer">GitHub ↗</a>
          <a className="btn" href={DEEPWIKI} target="_blank" rel="noopener noreferrer">DeepWiki ↗</a>
        </nav>
      </header>
      <main className="docs-main">
        <PageHeader title={section.title} description={section.summary} />
        <Docs />
      </main>
      <footer className="docs-foot">
        <span>VarunaSetu: integrated prototype, not production-ready.</span>
        <a href={GITHUB} target="_blank" rel="noopener noreferrer">Source repository</a>
      </footer>
    </div>
  );
}
