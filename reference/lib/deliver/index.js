// Delivery interface. Adapters expose async deliver(text) and report whether they're configured.
//   createDeliverer(name) -> { name, configured(), async deliver(text) }

import { createConsoleDeliverer } from './console.js';
import { createTelegramDeliverer } from './telegram.js';

export function createDeliverer(name) {
  switch ((name || 'console').toLowerCase()) {
    case 'console':
      return createConsoleDeliverer();
    case 'telegram':
      return createTelegramDeliverer();
    default:
      throw new Error(`Unknown DELIVER_TO "${name}". Use "console" or "telegram".`);
  }
}
