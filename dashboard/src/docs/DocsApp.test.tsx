import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';
import DocsApp from './DocsApp';

beforeEach(() => { window.location.hash = ''; });

describe('documentation page', () => {
  it('is a page of its own: masthead, wordmark, a way back, and no dashboard navigation', async () => {
    window.location.hash = '#/preamble';
    render(<DocsApp />);
    expect(await screen.findByRole('heading', { level: 1, name: 'Preamble' })).toBeInTheDocument();
    expect(screen.getAllByRole('img', { name: 'VarunaSetu' }).length).toBeGreaterThanOrEqual(2);
    expect(screen.getByRole('link', { name: '← Dashboard' })).toHaveAttribute('href', '/');
    expect(screen.getByRole('link', { name: /City simulator/ })).toHaveAttribute('target', '_blank');
    expect(screen.queryByRole('navigation', { name: 'Screens' })).not.toBeInTheDocument();
  });

  it('opens on the preamble when there is no route', async () => {
    render(<DocsApp />);
    expect(await screen.findByRole('heading', { level: 1, name: 'Preamble' })).toBeInTheDocument();
  });

  it('resolves :section/:page path params and falls back on unknown ones', async () => {
    window.location.hash = '#/documentation/verification';
    const { unmount } = render(<DocsApp />);
    expect(await screen.findByRole('heading', { level: 3, name: /^Reason codes/ })).toBeInTheDocument();
    unmount();
    window.location.hash = '#/how-it-works/nope';
    render(<DocsApp />);
    expect(await screen.findByText(/There is no page/)).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 2, name: 'The pipeline' })).toBeInTheDocument();
  });

  it('highlights a constitution article from the URL', async () => {
    window.location.hash = '#/constitution/iii';
    render(<DocsApp />);
    expect(await screen.findByRole('heading', { level: 2, name: 'Disagreement is held, not guessed' })).toBeInTheDocument();
    expect(document.getElementById('article-iii')).toHaveClass('target');
    expect(document.getElementById('article-i')).not.toHaveClass('target');
  });

  it('searches every page from ?q= and writes the query to the URL', async () => {
    window.location.hash = '#/?q=ESCROW_EXPIRED';
    render(<DocsApp />);
    expect(await screen.findByRole('heading', { level: 2, name: /results? for/ })).toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: /Verification|Escrow/ }).length).toBeGreaterThan(0);
    await userEvent.clear(screen.getByLabelText('Search documentation'));
    expect(window.location.hash).toBe('#/');
  });

  it('lets the H3 explainer flip a neighbour and change the verdict', async () => {
    window.location.hash = '#/how-it-works/h3-network';
    render(<DocsApp />);
    expect(await screen.findByText(/Released: ACCEPT/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Staged claim' }));
    expect(screen.getByText(/Held: 0 of 2/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Neighbour 1: dry' }));
    await userEvent.click(screen.getByRole('button', { name: 'Neighbour 2: dry' }));
    expect(screen.getByText(/Released: ACCEPT.*2 raining/)).toBeInTheDocument();
  });
});
