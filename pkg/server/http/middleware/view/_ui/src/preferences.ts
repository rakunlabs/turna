const SIDEBAR_KEY = "turna:view:sidebar-open";

export const readSidebarOpen = () => {
  if (typeof localStorage === "undefined") return true;

  try {
    const stored = localStorage.getItem(SIDEBAR_KEY);
    return stored === null ? true : stored === "true";
  } catch {
    return true;
  }
};

export const writeSidebarOpen = (open: boolean) => {
  if (typeof localStorage === "undefined") return;

  try {
    localStorage.setItem(SIDEBAR_KEY, String(open));
  } catch {
    // The sidebar must still toggle when storage is unavailable or full.
  }
};
