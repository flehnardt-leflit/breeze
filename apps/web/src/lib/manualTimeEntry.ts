type ManualEntryError = 'required' | 'invalid' | 'nonexistent' | 'ambiguous' | 'order' | 'future';
type Result = { ok: true; startedAt: string; endedAt: string; durationMinutes: number } | { ok: false; error: ManualEntryError };

// Resolve a wall clock time using the browser's local time zone. Checking only
// new Date(y,m,d,h,m) misses repeated times because Date silently picks one.
function resolveLocal(year: number, month: number, day: number, hour: number, minute: number):
  { instant: Date } | { error: 'nonexistent' | 'ambiguous' } {
  const wall = Date.UTC(year, month - 1, day, hour, minute);
  const offsets = new Set<number>();
  // Collect offsets on both sides of a transition (including non-hour shifts).
  for (let hours = -48; hours <= 48; hours += 6) {
    offsets.add(new Date(wall + hours * 3_600_000).getTimezoneOffset());
  }
  const matches = [...offsets].map((offset) => new Date(wall + offset * 60_000))
    .filter((d) => d.getFullYear() === year && d.getMonth() === month - 1 &&
      d.getDate() === day && d.getHours() === hour && d.getMinutes() === minute);
  if (matches.length === 0) return { error: 'nonexistent' };
  if (matches.length > 1) return { error: 'ambiguous' };
  return { instant: matches[0]! };
}

export function parseManualTimeEntry(date: string, start: string, end: string, overnight: boolean, now = new Date()): Result {
  if (!date || !start || !end) return { ok: false, error: 'required' };
  const dm = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  const sm = /^(\d{2}):(\d{2})$/.exec(start);
  const em = /^(\d{2}):(\d{2})$/.exec(end);
  if (!dm || !sm || !em) return { ok: false, error: 'invalid' };
  const [, ys, ms, ds] = dm;
  const year = Number(ys), month = Number(ms), day = Number(ds);
  const sh = Number(sm[1]), sn = Number(sm[2]), eh = Number(em[1]), en = Number(em[2]);
  if (year < 100 || month < 1 || month > 12 || sh > 23 || eh > 23 || sn > 59 || en > 59) return { ok: false, error: 'invalid' };
  const calendar = new Date(Date.UTC(year, month - 1, day));
  if (calendar.getUTCFullYear() !== year || calendar.getUTCMonth() !== month - 1 || calendar.getUTCDate() !== day) return { ok: false, error: 'invalid' };
  const from = resolveLocal(year, month, day, sh, sn);
  if ('error' in from) return { ok: false, error: from.error };
  if (overnight) calendar.setUTCDate(calendar.getUTCDate() + 1);
  const to = resolveLocal(calendar.getUTCFullYear(), calendar.getUTCMonth() + 1, calendar.getUTCDate(), eh, en);
  if ('error' in to) return { ok: false, error: to.error };
  const durationMinutes = (to.instant.getTime() - from.instant.getTime()) / 60_000;
  if (durationMinutes <= 0) return { ok: false, error: 'order' };
  if (from.instant > now || to.instant > now) return { ok: false, error: 'future' };
  return { ok: true, startedAt: from.instant.toISOString(), endedAt: to.instant.toISOString(), durationMinutes };
}
