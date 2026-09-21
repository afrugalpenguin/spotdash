const test = require('node:test');
test('deliberate failure', () => { throw new Error('ci red check'); });
