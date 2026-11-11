import type { ProvisionDevice, TelemetryPayload } from './api';

export type StoryId = 'honest' | 'inflated' | 'conflict';

export interface Story {
  id: StoryId;
  title: string;
  plain: string;      // one sentence anyone can follow
  radarMm: number;    // what the weather radar says
  expect: string;     // headline the backend should reach
}

export const STORIES: Story[] = [
  { id: 'honest', title: 'Honest storm', plain: 'It rains. Every roof reports what the rain should have produced.', radarMm: 8, expect: 'Everything is accepted.' },
  { id: 'inflated', title: 'Inflated claim', plain: 'It rains, but one owner claims six times more water than their roof could collect.', radarMm: 8, expect: 'The inflated claim is quarantined; honest roofs are accepted.' },
  { id: 'conflict', title: 'Radar says dry', plain: 'Radar sees almost no rain, yet neighbouring roofs all report some. Evidence disagrees.', radarMm: 0.1, expect: 'Held in escrow until more evidence arrives.' },
];

export interface Planned { device: ProvisionDevice; payload: TelemetryPayload; label: string; suspicious: boolean }

const round = (n: number) => Math.round(n * 100) / 100;

/** Litres a roof should collect from `mm` of rain: depth × area × runoff coefficient. */
export const expectedLitres = (mm: number, d: Pick<ProvisionDevice, 'roofAreaM2' | 'runoffCoeff'>) => mm * d.roofAreaM2 * d.runoffCoeff;

/** Turn a story into the signed readings the devices will send. The backend alone decides the verdicts. */
export function plan(story: Story, devices: ProvisionDevice[]): Planned[] {
  const main = devices[0]?.h3Index;
  const cell = devices.filter((d) => d.h3Index === main).slice(0, 6);
  const rain = story.id === 'conflict' ? 4 : story.radarMm;
  return cell.map((device, i) => {
    const liar = story.id === 'inflated' && i === cell.length - 1;
    const wobble = 0.94 + ((i * 37) % 11) / 100; // small deterministic spread, no randomness
    const claimed = round(expectedLitres(rain, device) * (liar ? 6 : wobble));
    return {
      device,
      label: liar ? 'inflated' : 'honest',
      suspicious: liar,
      payload: {
        building_id: device.buildingId, claimed_litres: claimed, stored_litres: claimed,
        roof_area_m2: device.roofAreaM2, runoff_coeff: device.runoffCoeff,
        capacity_litres: device.capacityLitres, radar_mm: story.radarMm, h3_index: device.h3Index,
      },
    };
  });
}
