// SPDX-License-Identifier: GPL-3.0-or-later
// A qualifying PostgreSQL selection must actually execute and pass its tests.
import {readFileSync} from 'node:fs';

const events = readFileSync(process.argv[2], 'utf8').trim().split('\n').map((line) => JSON.parse(line));
const tests = new Map();
for (const event of events) {
  if (event.Test) tests.set(`${event.Package}/${event.Test}`, event.Action);
}
if (tests.size === 0 || Array.from(tests.values()).some((action) => action !== 'pass')) {
  throw new Error('Required tests were empty, skipped, incomplete or failed');
}
console.info(`Verified ${tests.size} required test results`);
