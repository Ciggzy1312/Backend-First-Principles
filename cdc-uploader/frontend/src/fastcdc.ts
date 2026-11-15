import { sha256Hex } from "./lib/sha256";

// Content-defined chunking (FastCDC style), written by hand.
//
// Fixed-size parts (Uploader 1) break when bytes are inserted: every later part
// shifts and gets a new hash. Here the cut points are decided by the CONTENT
// around them, so after an insert the cut points a little further on land on the
// same bytes as before and those chunks keep their hashes.
//
// How a cut point is chosen: slide a "Gear" hash along the bytes. It is a
// rolling value that depends only on the last 32 bytes. Whenever its top bits
// are all zero, cut there. With n top bits that happens about once every 2^n
// bytes, so n sets the chunk size.
//
// Two refinements (from FastCDC):
//  - skip the first MIN_SIZE bytes of every chunk, so chunks are never tiny
//  - "normalized" chunking: use a STRICTER mask (17 bits) until AVG_SIZE and an
//    EASIER one (15 bits) after it, which pulls chunk sizes toward the average
//    instead of leaving a long tail of huge chunks
//  - always cut at MAX_SIZE if nothing matched

export const MIN_SIZE = 16 * 1024;
export const AVG_SIZE = 64 * 1024;
export const MAX_SIZE = 256 * 1024;

const MASK_STRICT = makeMask(17); // before AVG_SIZE: cuts are rarer
const MASK_EASY = makeMask(15); // after AVG_SIZE: cuts are likelier

// A mask for the TOP `bits` bits of a 32-bit value. Top bits are used because
// they depend on the most input bytes (the lowest bit only on the last byte).
function makeMask(bits: number): number {
  return (((1 << bits) - 1) << (32 - bits)) >>> 0;
}

// 256 random 32-bit numbers, one per possible byte value. The table must be
// IDENTICAL every time the app runs: if it changed, the same file would be cut
// differently and nothing already stored on the server would ever match again.
// So it comes from a fixed-seed generator (mulberry32), never Math.random().
const GEAR = (() => {
  let seed = 0x9e3779b9;
  const table = new Uint32Array(256);
  for (let i = 0; i < 256; i++) {
    seed = (seed + 0x6d2b79f5) >>> 0;
    let t = seed;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    table[i] = (t ^ (t >>> 14)) >>> 0;
  }
  return table;
})();

// Length of the next chunk in data[start..end). If fewer than MIN_SIZE bytes
// remain, the rest is one (final) chunk.
export function findCut(data: Uint8Array, start: number, end: number): number {
  let n = end - start;
  if (n <= MIN_SIZE) return n;
  if (n > MAX_SIZE) n = MAX_SIZE;
  const normal = Math.min(AVG_SIZE, n);

  let fp = 0;
  let i = MIN_SIZE; // bytes before this offset are never a cut point
  for (; i < normal; i++) {
    fp = ((fp << 1) + GEAR[data[start + i]]) >>> 0;
    if ((fp & MASK_STRICT) === 0) return i + 1;
  }
  for (; i < n; i++) {
    fp = ((fp << 1) + GEAR[data[start + i]]) >>> 0;
    if ((fp & MASK_EASY) === 0) return i + 1;
  }
  return n;
}

export type Chunk = {
  offset: number; // where the chunk starts in the file
  size: number;
  hash: string; // hex SHA-256 of the chunk's bytes
};

const READ_SIZE = 4 * 1024 * 1024; // how much of the file is held in memory at once

// Cuts a file into chunks, one at a time, reading it in 4 MiB windows so even a
// multi-GB file never sits in memory whole. Only offsets, sizes and hashes are
// kept; the chunk bytes are read again from the file when (and if) they need
// uploading.
export async function* chunkFile(
  file: Blob,
  signal?: AbortSignal,
): AsyncGenerator<Chunk> {
  let cursor = 0;
  while (cursor < file.size) {
    signal?.throwIfAborted();
    const end = Math.min(cursor + READ_SIZE, file.size);
    const win = new Uint8Array(await file.slice(cursor, end).arrayBuffer());
    const atEnd = end === file.size;

    // Cut inside this window while a full MAX_SIZE chunk still fits, otherwise
    // the cut point might depend on bytes we have not read yet. Near the end of
    // the window, stop and re-read starting from the cursor.
    let pos = 0;
    while (pos < win.length && (atEnd || win.length - pos >= MAX_SIZE)) {
      const size = findCut(win, pos, win.length);
      yield {
        offset: cursor + pos,
        size,
        hash: await sha256Hex(win.subarray(pos, pos + size)),
      };
      pos += size;
    }
    cursor += pos;
  }
}