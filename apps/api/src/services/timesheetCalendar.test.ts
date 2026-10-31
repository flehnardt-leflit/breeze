import { describe, expect, it } from 'vitest';
import { timesheetWeek } from './timesheetCalendar';

describe('timesheet calendar', () => {
  it('resolves the supported ISO year endpoints in UTC', () => {
    const early = timesheetWeek(new Date('0100-01-01T00:00:00Z'), 'UTC');
    expect(early.start.toISOString()).toBe('0100-01-01T00:00:00.000Z');
    expect(early.end.toISOString()).toBe('0100-01-08T00:00:00.000Z');
    const late = timesheetWeek(new Date('9999-12-22T00:00:00Z'), 'UTC');
    expect(late.start.toISOString()).toBe('9999-12-22T00:00:00.000Z');
    expect(late.end.toISOString()).toBe('9999-12-29T00:00:00.000Z');
  });
  it.each([
    ['2026-03-23', 'Europe/Berlin', '2026-03-22T23:00:00.000Z', '2026-03-29T22:00:00.000Z'],
    ['2026-10-19', 'Europe/Berlin', '2026-10-18T22:00:00.000Z', '2026-10-25T23:00:00.000Z'],
    ['2026-10-26', 'Europe/Berlin', '2026-10-25T23:00:00.000Z', '2026-11-01T23:00:00.000Z'],
    ['2026-03-02', 'America/New_York', '2026-03-02T05:00:00.000Z', '2026-03-09T04:00:00.000Z'],
    ['2018-10-29', 'America/Sao_Paulo', '2018-10-29T03:00:00.000Z', '2018-11-05T02:00:00.000Z'],
    ['2011-12-26', 'Pacific/Apia', '2011-12-26T10:00:00.000Z', '2012-01-01T10:00:00.000Z'],
  ])('resolves %s in %s with both local boundaries', (date, zone, start, end) => {
    const week = timesheetWeek(new Date(date + 'T00:00:00Z'), zone);
    expect(week.start.toISOString()).toBe(start);
    expect(week.end.toISOString()).toBe(end);
  });

  it('buckets Berlin midnight and Sunday/Monday correctly', () => {
    const week = timesheetWeek(new Date('2026-10-26T00:00:00Z'), 'Europe/Berlin');
    expect(week.dateOf(new Date('2026-10-25T22:30:00Z'))).toBe('2026-10-25');
    expect(week.dateOf(new Date('2026-10-25T23:30:00Z'))).toBe('2026-10-26');
    expect(week.dateOf(new Date('2026-10-29T23:30:00Z'))).toBe('2026-10-30');
    expect(week.days).toEqual(['2026-10-26', '2026-10-27', '2026-10-28', '2026-10-29', '2026-10-30', '2026-10-31', '2026-11-01']);
  });

  it('uses the actual first local instant when midnight is skipped', () => {
    const week = timesheetWeek(new Date('2018-11-04T00:00:00Z'), 'America/Sao_Paulo');
    expect(week.start.toISOString()).toBe('2018-11-04T03:00:00.000Z');
    expect(week.dateOf(week.start)).toBe('2018-11-04');
  });

  it('uses the first occurrence when a midnight rollback briefly crosses the date boundary', () => {
    const week = timesheetWeek(new Date('2009-11-01T00:00:00Z'), 'America/St_Johns');
    expect(week.start.toISOString()).toBe('2009-11-01T02:30:00.000Z');
    const previous = timesheetWeek(new Date('2009-10-25T00:00:00Z'), 'America/St_Johns');
    expect(previous.end.toISOString()).toBe('2009-11-01T03:30:00.000Z');
  });
});
