// Refreshes src/tlds.json from the IANA root-zone TLD list.
const IANA_URL = 'https://data.iana.org/TLD/tlds-alpha-by-domain.txt'

const response = await fetch(IANA_URL)
if (!response.ok) {
    throw new Error(`IANA fetch failed: ${response.status} ${response.statusText}`)
}
const text = await response.text()
const tlds = text
    .split('\n')
    .map(line => line.trim().toLowerCase())
    .filter(line => line && !line.startsWith('#'))

if (tlds.length < 1000) {
    throw new Error(`Suspiciously short TLD list (${tlds.length} entries) — refusing to overwrite`)
}

await Bun.write(new URL('../src/tlds.json', import.meta.url), JSON.stringify(tlds, null, 4) + '\n')
console.log(`Wrote ${tlds.length} TLDs`)
