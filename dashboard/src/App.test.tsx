import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { getAnalytics, listCells, listEscrows, listLedger, listReadings } from './api/client';
import App from './App';

vi.mock('react-leaflet', () => ({
  MapContainer: ({ children }: { children?: React.ReactNode }) => <div data-testid="map">{children}</div>,
  TileLayer: () => <div data-testid="tile" />,
  Polygon: ({ children }: { children?: React.ReactNode }) => <div data-testid="polygon">{children}</div>,
  Popup: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock('h3-js', () => ({ cellToBoundary: () => [[17.385, 78.4867], [17.386, 78.487], [17.385, 78.488]] }));
vi.mock('./api/client');

const mocked = vi.mocked({ getAnalytics, listCells, listEscrows, listLedger, listReadings });

beforeEach(() => {
  window.location.hash = '#/overview';
  mocked.listReadings.mockResolvedValue([{
    id: 'r1', deviceId: 'device-1', timestamp: '2026-01-01T00:00:00Z',
    h3Index: '8928308280fffff', rainfallMm: 4, radarRainfallMm: 4,
    peerConsensusMm: 4, peerCount: 4, decision: 'ACCEPT', reason: 'RADAR_AND_PEERS_AGREE',
  }]);
  mocked.listEscrows.mockResolvedValue([]);
  mocked.listLedger.mockResolvedValue([]);
  mocked.listCells.mockResolvedValue([{ h3Index: '8928308280fffff', state: 'ACCEPT', readings: 1 }]);
  mocked.getAnalytics.mockResolvedValue({
    totalReadings: 1, accepted: 1, provisional: 0, quarantined: 0,
    escrowResolutionRate: null, activeDevices: 1,
  });
});

describe('HydroLedger dashboard', () => {
  it('opens on the story view', async () => {
    window.location.hash = '';
    render(<App />);
    expect(await screen.findByText('Run story')).toBeInTheDocument();
  });

  it('renders live analytics and map data', async () => {
    render(<App />);
    expect(await screen.findByText('Total telemetry readings')).toBeInTheDocument();
    expect(screen.getByText('LIVE DATABASE')).toBeInTheDocument();
    window.location.hash = '#/map';
    expect(await screen.findByTestId('map')).toBeInTheDocument();
    expect(screen.getByTestId('polygon')).toBeInTheDocument();
  });

  it('links to the city simulator on port 8090 without calling it', async () => {
    render(<App />);
    const links = await screen.findAllByRole('link', { name: /open simulator/i });
    expect(links[0].getAttribute('href')).toContain('http://localhost:8090/');
    expect(links[0]).toHaveAttribute('target', '_blank');
  });

  it('keeps the same navigation and page header on every screen', async () => {
    render(<App />);
    for (const label of ['Story', 'Overview', 'Map', 'Readings', 'Escrows', 'Ledger']) {
      expect(screen.getByRole('link', { name: label })).toBeInTheDocument();
    }
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeInTheDocument();
  });

  it('shows an explicit backend error instead of static fixture data', async () => {
    mocked.listReadings.mockRejectedValue(new Error('telemetry API returned HTTP 503'));
    render(<App />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Live data unavailable');
    expect(screen.queryByText('SIMULATED DATA')).not.toBeInTheDocument();
  });
});
