export function canonical(path: string): string {
  if (!path.startsWith("/")) throw new Error("absolute path required");
  const segments = path.split(/[?#]/, 1)[0].split("/").filter(Boolean).map(segment => {
    const decoded = decodeURIComponent(segment);
    if (decoded.includes("/")) throw new Error("encoded slash");
    return decoded;
  });
  return "/" + segments.join("/");
}
