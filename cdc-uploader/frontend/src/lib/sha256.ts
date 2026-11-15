// Hex SHA-256 of a Blob or of raw bytes, using the browser's built-in Web
// Crypto. (crypto.subtle only exists on https:// pages and on localhost.)
export async function sha256Hex(
  data: Blob | Uint8Array<ArrayBuffer>,
): Promise<string> {
  const bytes = data instanceof Blob ? await data.arrayBuffer() : data;
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (b) =>
    b.toString(16).padStart(2, "0"),
  ).join("");
}