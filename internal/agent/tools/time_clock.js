// Installed before application scripts in a tool-owned tab. Animation frames,
// networking and workers retain their real clocks; this is a main-frame lab.
(() => {
  if (globalThis.top !== globalThis) return;
  const initial = __ATLAS_TIME__;
  const NativeDate = Date;
  let now = initial, next = 1, advancing = false;
  const timers = new Map();
  class LabDate extends NativeDate {
    constructor(...args) { super(...(args.length ? args : [now])); }
    static now() { return now; }
  }
  // Preserve Date()'s callable form as well as new Date().
  const ClockDate = new Proxy(LabDate, {
    apply() { return new NativeDate(now).toString(); }
  });
  globalThis.Date = ClockDate;
  const schedule = (fn, delay, interval, args) => {
    if (typeof fn !== 'function') throw new TypeError('Time lab requires function timers');
    if (timers.size >= 10000) throw new RangeError('Time lab timer limit exceeded');
    const ms = Math.max(interval ? 1 : 0, Math.min(2147483647, Number(delay) || 0));
    const id = next++;
    timers.set(id, { fn, args, due: now + ms, interval: interval ? ms : 0 });
    return id;
  };
  globalThis.setTimeout = (fn, delay, ...args) => schedule(fn, delay, false, args);
  globalThis.setInterval = (fn, delay, ...args) => schedule(fn, delay, true, args);
  globalThis.clearTimeout = globalThis.clearInterval = id => timers.delete(id);
  const api = Object.freeze({
    now: () => now,
    async advance(ms) {
      if (advancing) throw new Error('Clock advance already running');
      if (!Number.isSafeInteger(ms) || ms < 0 || ms > 31622400000) throw new RangeError('Invalid time advance');
      advancing = true;
      const end = now + ms;
      let fired = 0;
      try {
        await Promise.resolve();
        while (true) {
          let selected, timer;
          for (const [id, value] of timers) {
            if (value.due <= end && (!timer || value.due < timer.due || (value.due === timer.due && id < selected))) {
              selected = id; timer = value;
            }
          }
          if (!timer) break;
          if (++fired > 10000) throw new RangeError('Time lab callback budget exceeded');
          now = timer.due;
          if (timer.interval) timer.due += timer.interval;
          else timers.delete(selected);
          timer.fn(...timer.args);
          await Promise.resolve();
        }
        now = end;
        await Promise.resolve();
        return { now_ms: now, callbacks: fired, pending_timers: timers.size };
      } finally { advancing = false; }
    }
  });
  Object.defineProperty(globalThis, '__atlasTimeLab', { value: api, configurable: false, writable: false });
})();
