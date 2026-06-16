// Prints the digest to stdout. Default delivery — used for local testing and dry runs.

export function createConsoleDeliverer() {
  return {
    name: 'console',
    configured() { return true; },
    async deliver(text) {
      process.stdout.write('\n========== DIGEST ==========\n');
      process.stdout.write(text + '\n');
      process.stdout.write('============================\n');
      return { ok: true, channel: 'console' };
    },
  };
}
