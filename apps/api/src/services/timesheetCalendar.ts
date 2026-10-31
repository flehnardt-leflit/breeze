const DAY = 86_400_000;
const HOUR = 3_600_000;

/**
 * Builds a local calendar week and its UTC query bounds. Boundary search
 * selects the first instant of the calendar date; a skipped midnight starts at
 * the first representable time, and an entirely skipped date starts at the
 * next representable date. Unlike adding 168 hours this handles offset changes.
 */
export function timesheetWeek(calendarStart: Date, timeZone: string) {
  const formatter = new Intl.DateTimeFormat('en-US-u-ca-gregory-nu-latn', {
    timeZone, year: 'numeric', month: '2-digit', day: '2-digit',
  });
  const clock = new Intl.DateTimeFormat('en-US-u-ca-gregory-nu-latn', {
    timeZone, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
  });
  const dateOf = (instant: Date): string => {
    const parts = formatter.formatToParts(instant);
    const field = (type: Intl.DateTimeFormatPartTypes) => parts.find((part) => part.type === type)!.value;
    return `${field('year').padStart(4, '0')}-${field('month')}-${field('day')}`;
  };
  const days = Array.from({ length: 8 }, (_, i) => new Date(calendarStart.getTime() + i * DAY).toISOString().slice(0, 10));

  function boundaries(date: string): Date[] {
    // Every modern IANA offset is within 24 hours. Scan through a generous
    // 48-hour margin to detect skipped/repeated local midnights, then locate
    // the first matching millisecond inside the first matching hour.
    const naive = Date.parse(date + 'T00:00:00.000Z');
    // Try each offset in force around the date. This finds both occurrences
    // even when a rollback briefly reaches a date and then crosses back (e.g.
    // Newfoundland's 00:01 rollback), which hourly sampling alone would miss.
    const offsets = new Set<number>();
    for (let at = naive - 48 * HOUR; at <= naive + 48 * HOUR; at += 6 * HOUR) {
      const parts = clock.formatToParts(new Date(at));
      const get = (type: Intl.DateTimeFormatPartTypes) => Number(parts.find((part) => part.type === type)!.value);
      const wall = Date.UTC(get('year'), get('month') - 1, get('day'), get('hour'), get('minute'), get('second'));
      offsets.add(wall - at);
    }
    const matches = [...offsets].map(offset => naive - offset).filter(at => {
      const parts = clock.formatToParts(new Date(at));
      const get = (type: Intl.DateTimeFormatPartTypes) => parts.find((part) => part.type === type)!.value;
      return dateOf(new Date(at)) === date && get('hour') === '00' && get('minute') === '00' && get('second') === '00';
    }).sort((a, b) => a - b);
    if (matches.length) return matches.map(at => new Date(at));
    const lower = naive - 48 * HOUR;
    const upper = naive + 48 * HOUR;
    let previous = lower;
    for (let at = lower + HOUR; at <= upper; at += HOUR) {
      if (dateOf(new Date(at)) >= date) {
        let lo = previous, hi = at;
        while (hi - lo > 1) {
          const mid = lo + Math.floor((hi - lo) / 2);
          if (dateOf(new Date(mid)) >= date) hi = mid;
          else lo = mid;
        }
        return [new Date(hi)];
      }
      previous = at;
    }
    throw new RangeError('Unable to resolve calendar boundary');
  }

  const starts = boundaries(days[0]!);
  const ends = boundaries(days[7]!);
  return { start: starts[0]!, end: ends[ends.length - 1]!, days: days.slice(0, 7), dateOf };
}
