#!/usr/bin/env node
// faultproxy.mjs — a TCP fault-injection proxy for the live GUI/CLI tests.
//
//   node scripts/faultproxy.mjs --listen 19000 --target 127.0.0.1:9000 \
//       [--control 19001] [--mode direct|latency|throttle|blackhole|reset|flap] \
//       [--delay 250] [--bps 8192] [--fail-first 2]
//
// Data plane: a plain TCP proxy in front of the real MinIO. The fault mode
// decides what happens to the bytes:
//
//   direct     passthrough (control scenario)
//   latency    every TCP chunk is held --delay ms before forwarding
//   throttle   chunks are paced out at --bps bytes/second
//   blackhole  accepted, never answered — a dead endpoint (connections
//              established BEFORE the switch go dark: chunks are dropped)
//   reset      connections are killed with unread data pending → the OS
//              sends RST → the client surfaces ECONNRESET
//   flap       the first --fail-first connections each get a canned
//              `HTTP/1.1 503 Service Unavailable` and are closed; after
//              that the proxy passes through — a transient 5xx storm the
//              client must ride out with retries (or fail honestly on)
//
// Control plane: a tiny HTTP server so one long-lived proxy serves a whole
// scenario sequence without restarts (the S3 SDK keep-alive pool would
// otherwise survive mode changes made by restarting):
//
//   GET  /state                     → {"mode":...,"delayMs":...,"bps":...,
//                                     "connections":N,"active":N,"resets":N}
//   POST /mode {"mode":"latency",   → merged state (any subset of
//        "delayMs":400}               mode/delayMs/bps/failFirst;
//                                     reset/blackhole additionally break
//                                     live connections; setting mode/flap or
//                                     failFirst re-arms the flap counter)
//
// Fault decisions are made per-chunk from the CURRENT state (not per
// connection at accept time) so a keep-alive connection pooled under
// `direct` is fully affected by a later switch to latency/throttle.
import net from 'node:net';
import { createServer } from 'node:http';

const arg = (k, d) => {
  const i = process.argv.indexOf(`--${k}`);
  return i >= 0 ? process.argv[i + 1] : d;
};
const num = (k, d) => Number(arg(k, d));

const LISTEN = num('listen', 19000);
const CONTROL = num('control', LISTEN + 1);
const [THOST, TPORT] = String(arg('target', '127.0.0.1:9000')).split(':');

const state = {
  mode: arg('mode', 'direct'),
  delayMs: num('delay', 250),
  bps: num('bps', 8192),
  failFirst: num('fail-first', 0),
  flapped: 0,
  connections: 0,
  active: 0,
  resets: 0,
};

// every live client socket (blackhole holds them; reset kills them on switch)
const live = new Set();

const log = (m) => console.log(`[faultproxy] ${m}`);

const proxy = net.createServer((client) => {
  state.connections++;
  state.active++;
  const id = state.connections;
  const t0 = Date.now();
  let up = 0;
  let down = 0;
  let target = null; // dialed below unless blackhole/reset (TDZ-safe for finish)
  live.add(client);
  const finish = (why) => {
    if (!live.delete(client)) return; // finish runs once
    state.active--;
    client.destroy();
    if (target) target.destroy();
    log(`#${id} ${why} ${(Date.now() - t0) / 1000 | 0}s  c→t ${up}B  t→c ${down}B`);
  };
  client.on('close', () => finish('closed'));
  client.on('error', () => finish('error'));

  // blackhole never dials: from the client this is an endpoint that
  // accepted the handshake and then went silent forever
  if (state.mode === 'blackhole') {
    client.on('data', () => {}); // swallow requests (they are never read)
    return;
  }
  if (state.mode === 'reset') {
    // destroy with the request still unread → RST, not a clean FIN
    client.once('data', () => {
      state.resets++;
      state.active--;
      live.delete(client);
      client.destroy();
      log(`#${id} reset — killed with data pending (RST)`);
    });
    return;
  }
  // flap: a well-formed 503 for the first failFirst connections, then
  // passthrough. Connection-scoped by design — a canned response is a
  // complete HTTP reply, so the client must hang up and dial again, which
  // is exactly the retry loop this mode exists to exercise. The counter
  // resets whenever the control plane arms a new storm.
  if (state.mode === 'flap' && state.flapped < state.failFirst) {
    client.once('data', () => {
      state.flapped++;
      live.delete(client);
      state.active--;
      client.end(
        'HTTP/1.1 503 Service Unavailable\r\n' +
        'Content-Type: application/xml\r\n' +
        'Content-Length: 0\r\n' +
        'Connection: close\r\n' +
        '\r\n',
      );
      log(`#${id} flap ${state.flapped}/${state.failFirst} — canned 503`);
    });
    return;
  }

  target = net.connect(Number(TPORT), THOST);
  target.on('error', (e) => { log(`#${id} upstream error: ${e.message}`); finish('upstream-error'); });
  target.on('close', () => finish('closed'));

  // lane: one direction of the connection. Every chunk consults the
  // CURRENT fault state, so mode switches reach keep-alive connections.
  const lane = (from, to, tally) => {
    let queue = Buffer.alloc(0);
    let timer = null;
    const TICK = 100;
    const drain = () => {
      if (to.destroyed) { clearInterval(timer); timer = null; return; }
      const budget = Math.max(64, Math.round((state.bps * TICK) / 1000));
      if (queue.length === 0) { clearInterval(timer); timer = null; return; }
      const out = queue.subarray(0, budget);
      queue = queue.subarray(out.length);
      to.write(out);
    };
    from.on('data', (chunk) => {
      switch (state.mode) {
        case 'latency':
          setTimeout(() => { if (!to.destroyed) { to.write(chunk); tally(chunk.length); } }, state.delayMs);
          break;
        case 'throttle':
          queue = Buffer.concat([queue, chunk]);
          if (!timer) timer = setInterval(drain, TICK);
          break;
        case 'blackhole':
          break; // went dark mid-stream: swallow
        case 'reset':
          state.resets++;
          finish('reset-mid-stream');
          break;
        default:
          to.write(chunk);
          tally(chunk.length);
      }
    });
    from.on('close', () => { if (timer) clearInterval(timer); });
  };
  lane(client, target, (n) => { up += n; });
  lane(target, client, (n) => { down += n; });
});
proxy.on('error', (e) => { console.error(`[faultproxy] data plane: ${e.message}`); process.exit(1); });

const ctl = createServer((req, res) => {
  res.setHeader('content-type', 'application/json');
  if (req.method === 'GET' && req.url === '/state') {
    res.end(JSON.stringify(state));
    return;
  }
  if (req.method === 'POST' && req.url === '/mode') {
    let body = '';
    req.on('data', (c) => { body += c; });
    req.on('end', () => {
      try {
        const patch = JSON.parse(body || '{}');
        for (const k of ['mode', 'delayMs', 'bps', 'failFirst']) {
          if (patch[k] !== undefined) state[k] = patch[k];
        }
        // arming (or re-arming) a flap storm starts its counter from zero
        if (patch.mode === 'flap' || patch.failFirst !== undefined) state.flapped = 0;
        // reset/blackhole must also reach connections the SDK pooled while
        // the proxy was healthy — a kept-alive socket would otherwise stay
        // usable and the fault would never fire.
        if (state.mode === 'reset') {
          for (const s of live) {
            state.resets++;
            state.active--;
            s.destroy();
          }
          live.clear();
          log(`mode=reset — broke ${state.connections ? 'all live connections' : '(none live)'}`);
        }
        res.end(JSON.stringify(state));
      } catch (e) {
        res.statusCode = 400;
        res.end(JSON.stringify({ error: String(e.message) }));
      }
    });
    return;
  }
  res.statusCode = 404;
  res.end('{}');
});

proxy.listen(LISTEN, '127.0.0.1', () => {
  ctl.listen(CONTROL, '127.0.0.1', () => {
    log(`data :${LISTEN} → ${THOST}:${TPORT}  control :${CONTROL}  mode=${state.mode}` +
      (state.mode === 'latency' ? ` delay=${state.delayMs}ms` : '') +
      (state.mode === 'throttle' ? ` bps=${state.bps}` : '') +
      (state.mode === 'flap' ? ` fail-first=${state.failFirst}` : ''));
  });
});

const shutdown = () => {
  for (const s of live) s.destroy();
  live.clear();
  proxy.close();
  ctl.close();
  process.exit(0);
};
process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
