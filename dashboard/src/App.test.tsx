import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi } from 'vitest';

vi.mock('react-leaflet', () => ({
  MapContainer: ({ children }: any) => <div data-testid="map">{children}</div>,
  TileLayer: () => <div data-testid="tile" />,
  Polygon: ({ children }: any) => <div data-testid="polygon">{children}</div>,
  Popup: ({ children }: any) => <div>{children}</div>,
}));
vi.mock('h3-js', () => ({ cellToBoundary: () => [[17.385, 78.4867],[17.386,78.487],[17.385,78.488]] }));

import App from './App';

describe('HydroLedger dashboard', () => {
  it('shows SIMULATED data banner and analytics', async () => {
    render(<App />);
    expect(await screen.findByText(/SIMULATED DATA/i)).toBeInTheDocument();
    expect(await screen.findByText('Total telemetry readings')).toBeInTheDocument();
    expect(screen.getByText('Escrow panel')).toBeInTheDocument();
  });

  it('renders telemetry table with filter/sort/pagination controls', async () => {
    render(<App />);
    expect(await screen.findByLabelText('Telemetry readings table')).toBeInTheDocument();
    expect(screen.getByLabelText('Filter readings')).toBeInTheDocument();
    expect(screen.getByLabelText('Filter decision')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /next/i })).toBeInTheDocument();
  });

  it('filters and paginates table rows', async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByLabelText('Telemetry readings table');
    await user.selectOptions(screen.getByLabelText('Filter decision'), 'QUARANTINE');
    const table = screen.getByLabelText('Telemetry readings table');
    expect(within(table).getAllByText('QUARANTINE').length).toBeGreaterThan(0);
  });

  it('displays escrow cards with countdown/remaining and state', async () => {
    render(<App />);
    expect(await screen.findByText('Escrow panel')).toBeInTheDocument();
    expect((await screen.findAllByText(/Remaining/i)).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/e-00/).length).toBeGreaterThan(0);
  });

  it('renders H3 map polygons', async () => {
    render(<App />);
    expect(await screen.findByTestId('map')).toBeInTheDocument();
    expect((await screen.findAllByTestId('polygon')).length).toBeGreaterThan(0);
  });

  it('verifies ledger integrity via UI action', async () => {
    const user = userEvent.setup();
    render(<App />);
    const btn = await screen.findByRole('button', { name: /verify ledger integrity/i });
    await user.click(btn);
    expect(await screen.findByText(/SIMULATED structural linkage check passed/i)).toBeInTheDocument();
  });

  it('demonstration mode has 6 scripted scenarios', async () => {
    render(<App />);
    expect(await screen.findByText(/1\. Normal rainfall agreed by peers/)).toBeInTheDocument();
    expect(screen.getByText(/6\. Cross-cell collusion/)).toBeInTheDocument();
  });
});
