import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { parseManualTimeEntry } from './manualTimeEntry';

// Run in a separate process with TZ=Europe/Berlin to cover both DST transitions.
describe('manual time entry', () => {
  const originalTimezone = process.env.TZ;
  beforeAll(() => { process.env.TZ = 'Europe/Berlin'; });
  afterAll(() => {
    if (originalTimezone === undefined) delete process.env.TZ;
    else process.env.TZ = originalTimezone;
  });
  const now = new Date('2025-12-01T00:00:00Z');
  it('converts historical local times to instants', () => {
    expect(parseManualTimeEntry('2025-02-10', '10:00', '11:00', false, now)).toEqual({
      ok: true, startedAt: '2025-02-10T09:00:00.000Z', endedAt: '2025-02-10T10:00:00.000Z', durationMinutes: 60,
    });
  });
  it('rejects missing, invalid, future, and non-positive intervals', () => {
    expect(parseManualTimeEntry('', '10:00', '11:00', false, now)).toMatchObject({ ok: false, error: 'required' });
    expect(parseManualTimeEntry('2025-02-30', '10:00', '11:00', false, now)).toMatchObject({ ok: false, error: 'invalid' });
    expect(parseManualTimeEntry('2025-02-10', '11:00', '11:00', false, now)).toMatchObject({ ok: false, error: 'order' });
    expect(parseManualTimeEntry('2025-02-10', '11:00', '10:00', false, now)).toMatchObject({ ok: false, error: 'order' });
    expect(parseManualTimeEntry('2026-02-10', '10:00', '11:00', false, now)).toMatchObject({ ok: false, error: 'future' });
    expect(parseManualTimeEntry('2025-12-01', '00:30', '01:30', false, now)).toMatchObject({ ok: false, error: 'future' });
  });
  it('requires explicit overnight selection and calculates elapsed time over DST', () => {
    expect(parseManualTimeEntry('2025-03-29', '23:00', '04:00', false, now)).toMatchObject({ ok: false, error: 'order' });
    expect(parseManualTimeEntry('2025-03-29', '23:00', '04:00', true, now)).toMatchObject({ ok: true, durationMinutes: 240 });
  });
  it('rejects non-existent and repeated local times at both endpoints', () => {
    expect(parseManualTimeEntry('2025-03-30', '02:30', '04:00', false, now)).toMatchObject({ ok: false, error: 'nonexistent' });
    expect(parseManualTimeEntry('2025-03-30', '01:30', '02:30', false, now)).toMatchObject({ ok: false, error: 'nonexistent' });
    expect(parseManualTimeEntry('2025-10-26', '02:30', '04:00', false, now)).toMatchObject({ ok: false, error: 'ambiguous' });
    expect(parseManualTimeEntry('2025-10-26', '01:30', '02:30', false, now)).toMatchObject({ ok: false, error: 'ambiguous' });
  });
});
