'use strict';

const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(process.argv[2] || '');
if (!process.argv[2] || !fs.existsSync(path.join(ROOT, 'tools', 'web', 'engine.js'))) {
  console.error('нужен корень репозитория Raid Helper с tools/web/engine.js');
  process.exit(1);
}
const NAME = 'ManaCode_RaidHelper';
const BS = '\\';
const toc = fs.readFileSync(path.join(ROOT, NAME + '.toc'), 'utf8');
const VERSION = (toc.match(/^## Version:\s*(\S+)/m) || [])[1] || '0';
const OUT = path.resolve(process.argv[3] || path.join(process.env.LOCALAPPDATA || '', 'ManaCode', 'web', VERSION, NAME));
const ENGINE = path.join(ROOT, 'tools', 'web', 'engine');
const WASMOON = path.join(ROOT, 'node_modules', 'wasmoon');
const assets = require(path.join(ROOT, 'tools', 'web', 'assets.js'));

const files = toc.split(/\r?\n/).map((s) => s.trim())
  .filter((s) => s && !s.startsWith('#') && /\.lua$/i.test(s)).map((s) => s.split(BS).join('/'))
  .filter((s) => !s.startsWith('lead/'));

const ZONES = {
  icc: { map: 'IcecrownCitadel', load: 'IcecrownCitadel', rooms: ['council', 'deathbringer', 'deathwhisper', 'festergut',
    'frostmourne', 'gunship', 'lanathel', 'lichking', 'marrowgar', 'putricide', 'rotface', 'sindragosa', 'valithria'] },
  uld: { map: 'Ulduar', load: 'UlduarRaid', rooms: ['algalon', 'auriaya', 'freya', 'hodir', 'ignis', 'ironcouncil', 'kologarn',
    'leviathan', 'mimiron', 'razorscale', 'thorim', 'vezax', 'xt002', 'yogg'] },
  toc: { map: 'TheArgentColiseum', load: 'ArgentRaid', rooms: ['toc_anubarak', 'toc_arena'] },
  rs: { map: 'TheRubySanctum', load: 'RubySanctum', rooms: ['halion'] },
  naxx: { map: 'Naxxramas', rooms: [] },
  os: { map: 'TheObsidianSanctum', rooms: [] },
  eoe: { map: 'TheEyeofEternity', rooms: [] },
  ony: { map: 'OnyxiasLair', rooms: [] },
};
const ART = ['Interface', 'AddOns', NAME, 'art'].join(BS);
const zones = {};
for (const [k, z] of Object.entries(ZONES)) {
  const tex = z.rooms.map((r) => ART + BS + 'rooms' + BS + r + '.tga');
  if (z.load) tex.push(['Interface', 'Glues', 'LoadingScreens', 'LoadScreen' + z.load].join(BS));
  zones[k] = { map: z.map, tex };
}
const zoneOnly = (p) => /\\art\\rooms\\|\\LoadingScreens\\/i.test(p);
const ui = assets.uiSet().filter((p) => !zoneOnly(p));
ui.push(['Interface', 'AchievementFrame', 'UI-Achievement-Parchment-Horizontal'].join(BS));
const leadArt = path.join(ROOT, 'lead', 'art');
for (const f of fs.existsSync(leadArt) ? fs.readdirSync(leadArt).sort() : []) {
  if (/.(tga|blp)$/i.test(f)) ui.push(['Interface', 'AddOns', NAME, 'lead', 'art', f].join(BS));
}

const src = {};
for (const f of files) src[f] = fs.readFileSync(path.join(ROOT, f), 'utf8');
const spare = new Set();
for (const text of Object.values(src)) {
  for (const m of text.matchAll(/"((?:Interface|Spells|Textures|World|Environments|Creature)\\\\[^"\n]+)"/g)) {
    const p = m[1].split('\\\\').join('\\');
    if (/[\\\-_.]$/.test(p) || /\.\./.test(p) || zoneOnly(p)) continue;
    spare.add(p);
  }
}
for (const p of ui) spare.delete(p);

const numbers = new Set();
const walk = (d) => {
  for (const f of fs.readdirSync(d)) {
    const p = path.join(d, f);
    if (fs.statSync(p).isDirectory()) walk(p);
    else if (/\.lua$/i.test(f)) for (const m of fs.readFileSync(p, 'utf8').matchAll(/\b(\d{4,6})\b/g)) numbers.add(Number(m[1]));
  }
};
walk(path.join(ROOT, 'data'));

const achs = new Set();
const achDir = path.join(ROOT, 'data', 'achievements');
for (const f of fs.existsSync(achDir) ? fs.readdirSync(achDir) : []) {
  for (const m of fs.readFileSync(path.join(achDir, f), 'utf8').matchAll(/\bid = \{([^}]*)\}/g)) {
    for (const n of m[1].matchAll(/=\s*(\d+)/g)) achs.add(Number(n[1]));
  }
}

const items = {};
const cons = fs.readFileSync(path.join(ROOT, 'data', 'consumables.lua'), 'utf8');
for (const m of cons.matchAll(/item = "((?:[^"\\]|\\.)*)", id = (\d+)/g)) if (!items[m[2]]) items[m[2]] = m[1];

const manifest = {
  v: 1, name: NAME, version: VERSION, files, ui, spare: [...spare].sort(), zones,
  dataSpells: [...numbers].sort((a, b) => a - b), achs: [...achs].sort((a, b) => a - b), items,
  small: '^(interface/icons|addon/art/rooms|interface/glues/loadingscreens)/', spareMax: 64 * 1024,
};

fs.rmSync(OUT, { recursive: true, force: true });
const put = (rel, data) => {
  const f = path.join(OUT, rel);
  fs.mkdirSync(path.dirname(f), { recursive: true });
  fs.writeFileSync(f, data);
};
const copyDir = (rel) => {
  const from = path.join(ROOT, rel);
  if (!fs.existsSync(from)) return;
  for (const f of fs.readdirSync(from)) {
    const p = path.join(from, f);
    if (fs.statSync(p).isDirectory()) copyDir(path.join(rel, f));
    else put(path.join(rel, f), fs.readFileSync(p));
  }
};
put(NAME + '.toc', toc);
for (const [f, text] of Object.entries(src)) put(f, text);
copyDir('art');
copyDir(path.join('lead', 'art'));
put('web/wasmoon.js', fs.readFileSync(path.join(WASMOON, 'dist', 'index.js')));
put('web/glue.wasm', fs.readFileSync(path.join(WASMOON, 'dist', 'glue.wasm')));
put('web/wasmoon-LICENSE.txt', fs.readFileSync(path.join(WASMOON, 'LICENSE')));
for (const f of ['host.js', 'runtime.js', 'web.js', 'standalone.html', 'shim.lua', 'api.lua', 'boot.lua']) {
  put('web/' + f, fs.readFileSync(path.join(ENGINE, f)));
}
put('web/spells.lua', fs.readFileSync(path.join(ROOT, 'tools', 'lib', 'spellnames.lua')));
put('web/manifest.json', JSON.stringify(manifest, null, 1));
console.log(`${OUT}: версия ${VERSION}, файлов Lua ${files.length}, картинок ui ${ui.length}, запасных ${spare.size}, `
  + `чисел data/ ${numbers.size}, ачивок ${achs.size}, расходников ${Object.keys(items).length}`);
