// Node.js 22+. This example only reads Telegram; it never sends messages.
import { randomUUID } from 'node:crypto';

const token = process.env.AGENT_TELEGRAM_API_SECRET;
if (!token) throw new Error('Set AGENT_TELEGRAM_API_SECRET first');
const socket = new WebSocket(process.env.GATEWAY_URL || 'ws://127.0.0.1:8080/ws');
socket.addEventListener('open', () => {
  socket.send(JSON.stringify({ type: 'auth', token }));
});
socket.addEventListener('message', ({ data }) => {
  const frame = JSON.parse(data);
  console.log(JSON.stringify(frame));
  if (frame.type === 'ready') {
    for (const method of ['events.subscribe', 'status']) {
      socket.send(JSON.stringify({ type: 'request', id: randomUUID(), method, params: {} }));
    }
  }
});
socket.addEventListener('error', () => console.error('Gateway connection failed'));
socket.addEventListener('close', ({ code, reason }) => {
  console.error(`Gateway closed: ${code} ${reason}`);
});
process.on('SIGINT', () => socket.close(1000, 'client stopped'));
