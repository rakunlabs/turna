export function normalizeDeviceCode(value: string) {
  const flat = value.toUpperCase().replace(/[\s-]+/g, "");
  return flat.length === 8 ? `${flat.slice(0, 4)}-${flat.slice(4)}` : value.toUpperCase();
}

export function deviceLookupCurrent(requested: string, current: string) {
  return requested === normalizeDeviceCode(current);
}
