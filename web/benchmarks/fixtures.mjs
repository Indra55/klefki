import {createHash} from 'node:crypto';
import {deflateSync} from 'node:zlib';
import {writeFileSync} from 'node:fs';
import {join} from 'node:path';

const MiB = 1024 * 1024;
const crcTable = Array.from({length: 256}, (_, n) => {
  for (let i = 0; i < 8; i++) n = n & 1 ? 0xedb88320 ^ (n >>> 1) : n >>> 1;
  return n >>> 0;
});
function crc32(bytes) {
  let n = 0xffffffff;
  for (const b of bytes) n = crcTable[(n ^ b) & 255] ^ (n >>> 8);
  return (n ^ 0xffffffff) >>> 0;
}
function noise(size, seed) {
  const bytes = Buffer.alloc(size);
  let n = createHash('sha256').update(seed).digest().readUInt32LE(0) || 1;
  for (let i = 0; i < size; i++) {
    if (i % 4 === 0) { n ^= n << 13; n ^= n >>> 17; n ^= n << 5; }
    bytes[i] = n >>> ((i % 4) * 8);
  }
  return bytes;
}
function repeated(size, text) {
  const bytes = Buffer.alloc(size);
  bytes.fill(Buffer.from(text));
  return bytes;
}
function pngChunk(type, data) {
  const name = Buffer.from(type), length = Buffer.alloc(4), crc = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  crc.writeUInt32BE(crc32(Buffer.concat([name, data])));
  return Buffer.concat([length, name, data, crc]);
}
function png() {
  const width = 590, height = 590, pixels = noise(width * height * 3, 'png');
  const rows = Buffer.alloc(height * (width * 3 + 1));
  for (let y = 0; y < height; y++) pixels.copy(rows, y * (width * 3 + 1) + 1, y * width * 3, (y + 1) * width * 3);
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width); header.writeUInt32BE(height, 4); header[8] = 8; header[9] = 2;
  return Buffer.concat([Buffer.from('89504e470d0a1a0a', 'hex'), pngChunk('IHDR', header), pngChunk('IDAT', deflateSync(rows)), pngChunk('IEND', Buffer.alloc(0))]);
}
function zip() {
  const name = Buffer.from('payload.bin'), data = noise(MiB - 120, 'zip');
  const local = Buffer.alloc(30), central = Buffer.alloc(46), end = Buffer.alloc(22);
  local.writeUInt32LE(0x04034b50); local.writeUInt16LE(20, 4);
  local.writeUInt32LE(crc32(data), 14); local.writeUInt32LE(data.length, 18); local.writeUInt32LE(data.length, 22); local.writeUInt16LE(name.length, 26);
  central.writeUInt32LE(0x02014b50); central.writeUInt16LE(20, 4); central.writeUInt16LE(20, 6);
  central.writeUInt32LE(crc32(data), 16); central.writeUInt32LE(data.length, 20); central.writeUInt32LE(data.length, 24); central.writeUInt16LE(name.length, 28);
  end.writeUInt32LE(0x06054b50); end.writeUInt16LE(1, 8); end.writeUInt16LE(1, 10);
  end.writeUInt32LE(central.length + name.length, 12); end.writeUInt32LE(local.length + name.length + data.length, 16);
  return Buffer.concat([local, name, data, central, name, end]);
}
function pdf() {
  const stream = 'BT /F1 12 Tf 30 750 Td (Klefki benchmark PDF) Tj ET\n';
  const objects = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
    `<< /Length ${stream.length} >>\nstream\n${stream}endstream`,
  ];
  let text = '%PDF-1.4\n';
  const offsets = [];
  for (const [i, object] of objects.entries()) { offsets.push(text.length); text += `${i + 1} 0 obj\n${object}\nendobj\n`; }
  const start = text.length;
  text += 'xref\n0 6\n0000000000 65535 f \n' + offsets.map(n => String(n).padStart(10, '0') + ' 00000 n \n').join('');
  text += `trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n${start}\n%%EOF\n% padding\n`;
  return Buffer.from(text + '%'.repeat(MiB - text.length));
}

export function fixtures(dir, quick) {
  const specs = quick ? [
    ['tiny.txt', 'text/plain', () => repeated(1024, 'Klefki benchmark.\n')],
    ['text.txt', 'text/plain', () => repeated(MiB, 'Klefki relay benchmark. Local encryption and transfer.\n')],
    ['archive.zip', 'application/zip', zip],
  ] : [
    ['tiny.txt', 'text/plain', () => repeated(1024, 'Klefki benchmark.\n')],
    ['table.csv', 'text/csv', () => repeated(65536, 'id,value,description\n123,456,benchmark row\n')],
    ['text.txt', 'text/plain', () => repeated(MiB, 'Klefki relay benchmark. Local encryption and transfer.\n')],
    ['document.pdf', 'application/pdf', pdf],
    ['photo.png', 'image/png', png],
    ['archive.zip', 'application/zip', zip],
    ...[8, 32, 64].map(n => [`binary-${n}MiB.bin`, 'application/octet-stream', () => noise(n * MiB, `binary-${n}`)]),
  ];
  return specs.map(([name, mime, build]) => {
    const bytes = build(), path = join(dir, name);
    writeFileSync(path, bytes);
    return {name, mime, path, size: bytes.length, sha256: createHash('sha256').update(bytes).digest('hex')};
  });
}
