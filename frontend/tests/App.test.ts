import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import App from '../src/App.svelte';
import type { Snapshot } from '../bindings/github.com/openutm/utm-adapter/internal/adapter/models';

const api = vi.hoisted(() => ({
  GetSnapshot: vi.fn(), GetDeclarations: vi.fn(), GetFleet: vi.fn(),
  BeginSession: vi.fn(), EndSession: vi.fn(), ExportSession: vi.fn(),
  Synchronize: vi.fn(), ApplyConfig: vi.fn(), Stop: vi.fn(), Start: vi.fn(),
  ClearCredentials: vi.fn(), RemoveMissionPlannerPlugin: vi.fn(),
}));
vi.mock('../bindings/github.com/openutm/utm-adapter/internal/adapter', () => ({ Service: api }));

const declaration = '00000000-0000-0000-0000-000000000001';
const aircraft = '10000000-0000-0000-0000-000000000001';
const pilot = '20000000-0000-0000-0000-000000000001';
const operation = '30000000-0000-0000-0000-000000000001';
let snapshot: Snapshot;
const link = (state: string, detail: string) => ({ state, detail, checkedAt: new Date().toISOString() });

beforeEach(() => {
  vi.resetAllMocks();
  const now = new Date().toISOString();
  snapshot = {
    version: '0.2.0', running: true, now, frames: 123, parseErrors: 2,
    lastParseError: 'Checksum mismatch', sent: 0, events: [], sessions: [],
    configPath: '/test/product.json', bridgeURL: 'http://127.0.0.1:14552',
    config: { baseURL: 'http://blender.test', aerobridgeURL: 'http://fleet.test',
      aerobridgeEnabled: true, source: 'auto', transport: 'udp', address: '127.0.0.1:14551',
      authMode: 'bearer', autoConfigureGCS: true, systemID: 0,
      sessionDelivery: false, hasAerobridgeToken: false, bridgeAddress: '127.0.0.1:14552',
      tokenURL: '', clientID: '', scope: '', missionPlannerPlugin: false, serialDevice: '',
      baud: 57600, declarationID: '', vehicleID: '', operatorID: '', publish: false,
      hasToken: false, hasClientSecret: false },
    blender: link('connected', 'Reachable'), access: link('connected', 'Read access verified'),
    telemetry: link('connected', 'MAVLink frames are parsing'), delivery: link('idle', 'Disabled'),
    aerobridge: link('connected', 'Registry access verified'), records: link('connected', 'Stored locally'),
    restrictions: link('connected', 'Airspace restrictions refreshed'),
    gcs: [{ id: 'test-gcs', name: 'Third GCS', installed: true, running: false, setupDetail: 'Forwarding ready', settingsPath: '', setupState: 'ready', enhancementState: '', enhancementVersion: '', backupPath: '' }],
    vehicles: [{ systemID: 1, lastSeen: now, gpsAt: now, latitude: 47.5, longitude: 19,
      altitudeMSL: 120, altitudeWGS84: 164, speed: 2.1, fix: 3, satellites: 14,
      heartbeatAt: now, positionAt: now, frames: 123, armed: false, relativeAltitude: 10, heading: 90 }],
  };
  api.GetSnapshot.mockImplementation(async () => structuredClone(snapshot));
  api.GetDeclarations.mockResolvedValue([{ id: declaration }]);
  api.GetFleet.mockResolvedValue({ aircraft: [{ id: aircraft }], pilots: [{ id: pilot }], operations: [{ id: operation }] });
});
afterEach(cleanup);

async function open() { render(App); await screen.findByRole('heading', { name: 'Your connections are ready.' }); }

describe('status and verification dashboard', () => {
  it('shows both services, parsed telemetry and parser diagnostics, then updates outages and stale vehicles', async () => {
    await open();
    expect(screen.getByText('Server reachable')).toBeTruthy();
    expect(screen.getByText('Registry reachable')).toBeTruthy();
    expect(screen.getByText('1 vehicle connected')).toBeTruthy();
    expect(screen.getByText('47.500000, 19.000000')).toBeTruthy();
    expect(screen.getByText('123 frames · 2 parse errors')).toBeTruthy();
    expect(screen.getByText('Last parse error: Checksum mismatch')).toBeTruthy();
    snapshot.access = link('error', 'Token expired');
    snapshot.aerobridge = link('error', 'Fleet unreachable');
    snapshot.telemetry = link('waiting', 'Waiting for telemetry');
    snapshot.vehicles![0].lastSeen = '2020-01-01T00:00:00Z';
    await screen.findByText('Token expired', {}, { timeout: 3000 });
    expect(screen.getByText('Fleet unreachable')).toBeTruthy();
    expect(screen.getByText('No live vehicle yet')).toBeTruthy();
  });

  it('selects a registered third GCS and clears typed secrets after saving settings', async () => {
    await open(); const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Connection settings' }));
    await user.selectOptions(screen.getByLabelText('Telemetry source'), 'test-gcs');
    await user.type(screen.getByLabelText('Bearer token', { exact: true }), 'secret');
    await user.click(screen.getByRole('button', { name: /Save & connect/ }));
    await waitFor(() => expect(api.ApplyConfig).toHaveBeenCalled());
    expect(api.ApplyConfig.mock.calls[0][0].source).toBe('test-gcs');
    await waitFor(() => expect((screen.getByLabelText('Bearer token', { exact: true }) as HTMLInputElement).value).toBe(''));
  });

  it('reports service errors and lets the user retry', async () => {
    await open(); const user = userEvent.setup();
    api.Stop.mockRejectedValueOnce(new Error('Receiver could not stop'));
    await user.click(screen.getByRole('button', { name: 'Pause connections' }));
    expect((await screen.findByRole('alert')).textContent).toContain('Receiver could not stop');
    await user.click(screen.getByRole('button', { name: 'Dismiss error' }));
    await user.click(screen.getByRole('button', { name: 'Pause connections' }));
    await waitFor(() => expect(api.Stop).toHaveBeenCalledTimes(2));
  });
});

describe('fleet assignment and compliance records', () => {
  it('loads populated choices, records the chosen assignment, ends and exports the session', async () => {
    await open(); const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Flight sessions' }));
    await waitFor(() => expect(api.GetFleet).toHaveBeenCalledOnce());
    for (const [id, value] of [['aircraft', aircraft], ['pilots', pilot], ['operations', operation], ['declarations', declaration]]) {
      expect(document.querySelector(`#${id} option`)?.getAttribute('value')).toBe(value);
    }
    for (const [label, value] of [['Declaration UUID', declaration], ['Aircraft ID', aircraft], ['Operator ID', 'operator'], ['Pilot ID', pilot], ['Pilot name', 'Pilot One'], ['Pilot licence', 'LIC-1'], ['Pilot contact', 'pilot@example.test'], ['Aerobridge operation UUID', operation]]) {
      await user.type(screen.getByLabelText(label, { exact: true }), value);
    }
    api.BeginSession.mockImplementation(async (a) => {
      // Clone just as Wails serializes arguments; later form edits cannot mutate a session.
      snapshot.sessions = [{ session: { id: 'session-1', assignment: JSON.parse(JSON.stringify(a)), endedAt: null, startedAt: new Date().toISOString(), blenderURL: snapshot.config.baseURL, aerobridgeURL: snapshot.config.aerobridgeURL, revision: 1 }, samples: 0, blenderPending: true, aerobridgePending: true }] as Snapshot['sessions'];
    });
    await user.click(screen.getByRole('button', { name: 'Start recording' }));
    await screen.findByRole('button', { name: 'End session' });
    expect(api.BeginSession.mock.calls[0][0]).toMatchObject({ vehicleID: aircraft, pilot: { id: pilot, name: 'Pilot One', license: 'LIC-1', contact: 'pilot@example.test' }, aerobridgeOperationID: operation });
    api.EndSession.mockImplementation(async () => { snapshot.sessions![0].session.endedAt = new Date().toISOString(); });
    await user.click(screen.getByRole('button', { name: 'End session' }));
    await screen.findByRole('button', { name: 'Export JSON report' });
    expect(api.EndSession).toHaveBeenCalledWith('session-1');
    api.ExportSession.mockResolvedValue('/test/exports/session-1.json');
    await user.click(screen.getByRole('button', { name: 'Export JSON report' }));
    expect((await screen.findByRole('status')).textContent).toContain('/test/exports/session-1.json');
  });

  it('clears stale fleet choices when the optional registry is disabled', async () => {
    await open(); const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Flight sessions' }));
    await waitFor(() => expect(document.querySelector('#aircraft option')).toBeTruthy());
    snapshot.config.aerobridgeEnabled = false;
    await user.click(screen.getByRole('button', { name: 'Synchronize now' }));
    await waitFor(() => expect(screen.queryByLabelText('Aerobridge operation UUID')).toBeNull());
    await user.click(screen.getByRole('button', { name: 'Refresh choices' }));
    await waitFor(() => expect(document.querySelector('#aircraft option')).toBeNull());
    expect(api.GetFleet).toHaveBeenCalledOnce();
  });
});
