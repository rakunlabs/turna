import type { Config, Group } from "@/store/store";

export type NavigationType = "grpc" | "swagger" | "page" | "iframe";

export type NavigationItem = {
  name: string;
  type: NavigationType;
  path: string;
  href: string;
  breadcrumb: string;
};

type NavigationVisit = {
  count: number;
  lastVisited: number;
};

const VISITS_KEY = "turna:view:navigation-visits";

export const sanitizePath = (value: string) => value.replace(/^\/+/, "");

export const routeHref = (type: NavigationType, path: string) =>
  `#/${type}/${sanitizePath(path)}`;

export const routePath = (type: NavigationType, path: string) =>
  `/${type}/${encodeURI(sanitizePath(path))}`;

const readNavigationVisits = () => {
  if (typeof localStorage === "undefined") return {} as Record<string, NavigationVisit>;

  try {
    return JSON.parse(localStorage.getItem(VISITS_KEY) ?? "{}") as Record<
      string,
      NavigationVisit
    >;
  } catch {
    return {} as Record<string, NavigationVisit>;
  }
};

export const recordNavigationVisit = (href: string) => {
  if (typeof localStorage === "undefined") return;

  try {
    const visits = readNavigationVisits();
    const previous = visits[href];
    visits[href] = {
      count: Number.isFinite(previous?.count) ? previous.count + 1 : 1,
      lastVisited: Date.now(),
    };
    localStorage.setItem(VISITS_KEY, JSON.stringify(visits));
  } catch {
    // Navigation should still work when storage is unavailable or full.
  }
};

// Ordered by last visit, so whatever was just opened comes back on top. Visit
// count only breaks ties between entries stored in the same millisecond.
export const recentNavigation = (items: NavigationItem[], limit = 10) => {
  const visits = readNavigationVisits();

  return items
    .filter((item) => Number.isFinite(visits[item.href]?.lastVisited))
    .sort((a, b) => {
      const aVisit = visits[a.href];
      const bVisit = visits[b.href];
      return bVisit.lastVisited - aVisit.lastVisited || bVisit.count - aVisit.count;
    })
    .slice(0, limit);
};

const addItem = (
  items: NavigationItem[],
  type: NavigationType,
  path: string | undefined,
  name: string | undefined,
  breadcrumb: string
) => {
  if (!path) return;

  items.push({
    name: name ?? type,
    type,
    path,
    href: routeHref(type, path),
    breadcrumb,
  });
};

const addGroups = (
  items: NavigationItem[],
  groups: Group[] | undefined,
  parentPath = "",
  parents: string[] = []
) => {
  for (const group of groups ?? []) {
    const groupPath = `${parentPath}/${group.name}`;
    const groupNames = [...parents, group.name];

    for (const service of group.services ?? []) {
      const servicePath = `${groupPath}/${service.name}`;
      const breadcrumb = [...groupNames, service.name].join(" / ");

      for (const swagger of service.swagger ?? []) {
        addItem(
          items,
          "swagger",
          `${servicePath}/${swagger.name ?? "swagger"}`,
          swagger.name,
          breadcrumb
        );
      }
      for (const grpc of service.grpc ?? []) {
        addItem(
          items,
          "grpc",
          `${servicePath}/${grpc.name ?? "grpc"}`,
          grpc.name,
          breadcrumb
        );
      }
      for (const page of service.page ?? []) {
        addItem(
          items,
          "page",
          `${servicePath}/${(page.path ?? "page") + (page.path_extra ?? "")}`,
          page.name,
          breadcrumb
        );
      }
      for (const iframe of service.iframe ?? []) {
        addItem(
          items,
          "iframe",
          `${servicePath}/${iframe.path ?? "iframe"}`,
          iframe.name,
          breadcrumb
        );
      }
    }

    addGroups(items, group.groups, groupPath, groupNames);
  }
};

export const buildNavigation = (config: Config) => {
  const items: NavigationItem[] = [];

  for (const iframe of config.iframe ?? []) {
    addItem(items, "iframe", iframe.path, iframe.name, "Iframes");
  }
  for (const page of config.page ?? []) {
    addItem(
      items,
      "page",
      page.path ? page.path + (page.path_extra ?? "") : undefined,
      page.name,
      "Pages"
    );
  }
  for (const grpc of config.grpc ?? []) {
    addItem(items, "grpc", grpc.name, grpc.name, "gRPC APIs");
  }
  for (const swagger of config.swagger ?? []) {
    addItem(items, "swagger", swagger.name, swagger.name, "Swagger APIs");
  }

  addGroups(items, config.groups);
  return items;
};
