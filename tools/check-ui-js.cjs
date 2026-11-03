// CI-only syntax check for the browser JavaScript embedded in the Go binary.
// Does not execute the script or add any runtime dependency to LAN Bridge.
const fs = require('node:fs');
const vm = require('node:vm');
const html = fs.readFileSync('internal/ui/embed/index.html', 'utf8');
const match = html.match(/<script>([\s\S]*?)<\/script>/);
if (!match) throw new Error('Embedded UI script not found');
new vm.Script(match[1], {filename: 'internal/ui/embed/index.html:inline.js'});
console.log('Embedded UI JavaScript syntax PASS');
